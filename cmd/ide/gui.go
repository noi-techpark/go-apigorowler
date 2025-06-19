// SPDX-FileCopyrightText: 2024 NOI Techpark <digital@noi.bz.it>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gdamore/tcell/v2"
	crawler "github.com/noi-techpark/go-apigorowler"
	"github.com/rivo/tview"
	"gopkg.in/yaml.v3"
)

var debounceTimer *time.Timer
var debounceMutex sync.Mutex

type ConsoleLogger struct {
	LogFunc func(msg string)
}

func (cl ConsoleLogger) Info(msg string, args ...any) {
	cl.LogFunc(fmt.Sprintf("[INFO] "+msg, args...))
}

func (cl ConsoleLogger) Debug(msg string, args ...any) {
	cl.LogFunc(fmt.Sprintf("[#bdc9c4] "+msg, args...))
}

func (cl ConsoleLogger) Warning(msg string, args ...any) {
	cl.LogFunc(fmt.Sprintf("[orange] "+msg, args...))
}

func (cl ConsoleLogger) Error(msg string, args ...any) {
	cl.LogFunc(fmt.Sprintf("[red] "+msg, args...))
}

type ConsoleApp struct {
	app            *tview.Application
	watcher        *fsnotify.Watcher
	selectedStep   int
	mutex          sync.Mutex
	execLog        *tview.TextView
	description    *tview.TextView
	partialResult  *tview.TextView
	fullResult     *tview.TextView
	stepList       *tview.List
	configFilePath string
	profilerDAta   []crawler.StepProfilerData
	stopFn         context.CancelFunc
}

func NewConsoleApp() *ConsoleApp {
	return &ConsoleApp{
		app:          tview.NewApplication(),
		selectedStep: 0,
		profilerDAta: make([]crawler.StepProfilerData, 0),
	}
}

func (c *ConsoleApp) Run() {
	var inputField *tview.InputField

	inputField = tview.NewInputField().
		SetLabel("Enter path to configuration file: ").
		SetFieldWidth(40).
		SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEnter {
				c.validateAndGotoIDE(inputField)
			}
		})

	form := tview.NewForm().
		AddFormItem(inputField).
		AddButton("OK", func() {
			c.validateAndGotoIDE(inputField)
		})

	form.SetBorder(true).SetTitle("Configuration Input").SetTitleAlign(tview.AlignLeft)

	c.app.SetRoot(form, true)

	if err := c.app.Run(); err != nil {
		log.Fatal(err)
	}
}

func (c *ConsoleApp) validateAndGotoIDE(inputField *tview.InputField) {
	path := inputField.GetText()
	if _, err := os.Stat(path); err != nil {
		inputField.SetLabel("Invalid path. Try again: ")
		c.app.Draw() // Refresh UI after label change
		return
	}
	c.configFilePath = path
	c.gotoIDE(path)
	go func() {
		c.onConfigFileChanged()
	}()
}

func (c *ConsoleApp) gotoIDE(path string) {
	var err error
	c.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	err = c.watcher.Add(path)
	if err != nil {
		log.Fatal(err)
	}

	dumpButton := tview.NewButton("Dump Steps").
		SetSelectedFunc(func() {
			c.dumpStepsToLog()
		})
	dumpButton.SetBorder(true)

	stopButton := tview.NewButton("Stop").
		SetSelectedFunc(func() {
			c.stopExec()
		})
	stopButton.SetBorder(true)

	c.execLog = tview.NewTextView()
	c.execLog.SetDynamicColors(true)
	c.execLog.SetScrollable(true)
	c.execLog.SetBorder(true)
	c.execLog.SetTitle("Execution Log")

	c.description = tview.NewTextView()
	c.description.SetDynamicColors(true)
	c.description.SetBorder(true)
	c.description.SetTitle("Step Context")

	c.partialResult = tview.NewTextView()
	c.partialResult.SetDynamicColors(true)
	c.partialResult.SetScrollable(true)
	c.partialResult.SetBorder(true)
	c.partialResult.SetTitle("Step Output")

	c.fullResult = tview.NewTextView()
	c.fullResult.SetDynamicColors(true)
	c.fullResult.SetScrollable(true)
	c.fullResult.SetBorder(true)
	c.fullResult.SetTitle("Result")

	c.stepList = tview.NewList()
	c.stepList.SetBorder(true)
	c.stepList.SetTitle("Steps")

	c.app.EnableMouse(true)

	c.fullResult.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		x, y := c.fullResult.GetScrollOffset()
		switch event.Key() {
		case tcell.KeyUp:
			c.fullResult.ScrollTo(x, y-1)
		case tcell.KeyDown:
			c.fullResult.ScrollTo(x, y+1)
		case tcell.KeyPgUp:
			c.fullResult.ScrollTo(x, y-5)
		case tcell.KeyPgDn:
			c.fullResult.ScrollTo(x, y+5)
		}
		return event
	})

	c.execLog.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		x, y := c.execLog.GetScrollOffset()
		switch event.Key() {
		case tcell.KeyUp:
			c.execLog.ScrollTo(x, y-1)
		case tcell.KeyDown:
			c.execLog.ScrollTo(x, y+1)
		case tcell.KeyPgUp:
			c.execLog.ScrollTo(x, y-5)
		case tcell.KeyPgDn:
			c.execLog.ScrollTo(x, y+5)
		}
		return event
	})

	c.partialResult.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		x, y := c.partialResult.GetScrollOffset()
		switch event.Key() {
		case tcell.KeyUp:
			c.partialResult.ScrollTo(x, y-1)
		case tcell.KeyDown:
			c.partialResult.ScrollTo(x, y+1)
		case tcell.KeyPgUp:
			c.partialResult.ScrollTo(x, y-5)
		case tcell.KeyPgDn:
			c.partialResult.ScrollTo(x, y+5)
		}
		return event
	})

	focusOrder := []tview.Primitive{c.stepList, c.partialResult, c.fullResult}
	currentFocus := 0

	c.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyTAB {
			currentFocus = (currentFocus + 1) % len(focusOrder)
			c.app.SetFocus(focusOrder[currentFocus])
			return nil
		}
		return event
	})

	c.stepList.SetChangedFunc(func(index int, _ string, _ string, _ rune) {
		if index < 0 || index >= len(c.profilerDAta) {
			return // avoid panic
		}

		c.mutex.Lock()
		defer c.mutex.Unlock()
		c.selectedStep = index
		data := c.profilerDAta[index]
		conf, _ := json.MarshalIndent(data.Config, "", "  ")
		// ctx, _ := json.MarshalIndent(data.Context, "", "  ")
		d, _ := json.MarshalIndent(data.Data, "", "  ")
		descriptionText := ""
		descriptionText += fmt.Sprintf("[green]Step Name:[white:#308003]%s[-:-:-:-]\n", data.Name)
		for k, v := range data.Extra {
			descriptionText += fmt.Sprintf("[green]%s:[white:#308003]%s[-:-:-:-]\n", k, v)
		}
		descriptionText += fmt.Sprintf("[green]Step Configuration:\n[white:#308003]%s[-:-:-:-]\n", conf)

		escapedDescription := strings.NewReplacer("[]", "[[]").Replace(descriptionText)
		escapedPartial := strings.NewReplacer("[]", "[[]").Replace(string(d))
		c.description.SetText(escapedDescription)
		c.partialResult.SetText(escapedPartial)

		c.description.ScrollToBeginning()
		c.partialResult.ScrollToBeginning()
	})

	center := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(c.description, 0, 1, false).
		AddItem(c.partialResult, 0, 1, false)

	mainFlex := tview.NewFlex().
		AddItem(c.stepList, 50, 1, true).
		AddItem(center, 0, 2, false).
		AddItem(c.fullResult, 0, 3, false)

	execRow := tview.NewFlex().
		AddItem(c.execLog, 0, 1, false).
		AddItem(stopButton, 15, 0, false).
		AddItem(dumpButton, 15, 0, false)

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(execRow, 7, 0, false).
		AddItem(mainFlex, 0, 1, true)

	// Switch UI to main layout (no second Run call!)
	c.app.SetRoot(layout, true).SetFocus(c.stepList)

	go func() {
		for {
			select {
			case event := <-c.watcher.Events:
				if event.Op&fsnotify.Write == fsnotify.Write {
					// watcher can fire multiple events for the same save
					debounceMutex.Lock()
					if debounceTimer != nil {
						debounceTimer.Stop()
					}
					debounceTimer = time.AfterFunc(300*time.Millisecond, func() {
						c.onConfigFileChanged()
					})
					debounceMutex.Unlock()
				}
			case err := <-c.watcher.Errors:
				log.Println("Watcher error:", err)
			}
		}
	}()
}

func (c *ConsoleApp) appendLog(log string) {
	c.app.QueueUpdateDraw(func() {
		old := c.execLog.GetText(false)
		newLog := old
		if len(newLog) != 0 {
			newLog += "\n"
		}
		newLog += log

		c.execLog.SetText(newLog)
		c.execLog.ScrollToEnd()
	})
}

func (c *ConsoleApp) setupCrawlJob() {
	c.profilerDAta = make([]crawler.StepProfilerData, 0)
	if c.stopFn != nil {
		c.stopFn()
	}

	go func() {
		craw, _, _ := crawler.NewApiCrawler(c.configFilePath)
		craw.SetLogger(ConsoleLogger{
			LogFunc: func(msg string) {
				c.appendLog(msg)
			},
		})
		profiler := craw.EnableProfiler()
		defer close(profiler)
		go func() {
			for d := range profiler {
				c.profilerDAta = append(c.profilerDAta, d)
				c.stepList.AddItem(d.Name, "", 0, nil)
			}
		}()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		c.stopFn = cancel

		// handle stream
		if craw.Config.Stream {
			stream := craw.GetDataStream()
			go func() {
				for d := range stream {
					old := c.fullResult.GetText(false)

					output, _ := json.MarshalIndent(d, "", "   ")
					escapedresult := strings.NewReplacer("[]", "[[]").Replace(string(output))
					newTest := old
					if len(old) != 0 {
						newTest += "\n---\n"
					}
					newTest += escapedresult
					c.fullResult.SetText(newTest)
				}
			}()
		}

		err := craw.Run(ctx)

		if err != nil {
			c.appendLog("[red]" + err.Error())
		} else {
			if !craw.Config.Stream {
				res := craw.GetData()
				c.app.QueueUpdateDraw(func() {
					output, _ := json.MarshalIndent(res, "", "   ")
					escapedresult := strings.NewReplacer("[]", "[[]").Replace(string(output))
					c.fullResult.SetText(escapedresult)
				})
			} else {
				close(craw.GetDataStream())
			}
			c.appendLog("[green]Crawler run completed successfully")
		}
	}()
}

func (c *ConsoleApp) onConfigFileChanged() {
	c.description.SetText("")
	c.partialResult.SetText("")
	c.fullResult.SetText("")
	c.stepList.Clear()

	data, err := os.ReadFile(c.configFilePath)
	if err != nil {
		log.Printf("Error reading config file: %v", err)
		c.app.QueueUpdateDraw(func() {
			c.execLog.SetText(fmt.Sprintf("[red]Error reading config file: %v", err))
		})
		return
	}

	var cfg crawler.Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		c.appendLog("[red]" + err.Error())
		return
	}

	errors := crawler.ValidateConfig(cfg)
	if len(errors) != 0 {
		text := "[red]"
		for _, r := range errors {
			text += r.Error() + "\n"
		}

		c.appendLog(text)
		return
	}

	// If config valid, update UI or state here, also inside QueueUpdateDraw()
	c.appendLog("[green]Config validated successfully")
	c.setupCrawlJob()
}

func (c *ConsoleApp) dumpStepsToLog() {
	const dumpDir = "out"

	// Ensure directory exists
	err := os.MkdirAll(dumpDir, 0755)
	if err != nil {
		c.appendLog(fmt.Sprintf("[red]Failed to create output directory: %v", err))
		return
	}

	// Clear existing files
	files, err := os.ReadDir(dumpDir)
	if err != nil {
		c.appendLog(fmt.Sprintf("[red]Failed to read output directory: %v", err))
		return
	}
	for _, file := range files {
		os.Remove(filepath.Join(dumpDir, file.Name()))
	}

	// Dump steps
	c.mutex.Lock()
	defer c.mutex.Unlock()

	for i, step := range c.profilerDAta {
		b, err := json.MarshalIndent(step, "", "  ")
		if err != nil {
			c.appendLog(fmt.Sprintf("[red]Failed to marshal step %d: %v", i, err))
			continue
		}

		filename := filepath.Join(dumpDir, fmt.Sprintf("step_%d.json", i))
		err = os.WriteFile(filename, b, 0644)
		if err != nil {
			c.appendLog(fmt.Sprintf("[red]Failed to write step %d: %v", i, err))
			continue
		}
	}

	go func() {
		c.appendLog(fmt.Sprintf("[green]Dumped %d steps to %s", len(c.profilerDAta), dumpDir))
	}()
}

func (c *ConsoleApp) stopExec() {
	if nil == c.stopFn {
		return
	}

	c.stopFn()

	go func() {
		c.appendLog("[orange]Execution stopped")
	}()
}

func main() {
	app := NewConsoleApp()
	app.Run()
}
