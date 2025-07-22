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
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gdamore/tcell/v2"
	"github.com/noi-techpark/go-apigorowler"
	"github.com/rivo/tview"
	"github.com/sergi/go-diff/diffmatchpatch"
	"gopkg.in/yaml.v3"
)

var debounceTimer *time.Timer
var debounceMutex sync.Mutex

func escapeBrackets(input string) string {
	return strings.NewReplacer(
		"[", "[\u200B",
		"]", "\u200B]",
	).Replace(input)
}

// Generate a tview-color-tagged diff from before/after strings
func getColoredDiff(before, after string) string {
	// If there's no previous content, treat all as new
	if before == "" {
		return escapeBrackets(after)
	}

	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(before, after, false)

	var result strings.Builder
	for _, d := range diffs {
		switch d.Type {
		case diffmatchpatch.DiffInsert:
			// Green background for additions
			result.WriteString(`[black:green]` + escapeBrackets(d.Text) + `[-:-:-]`)
		case diffmatchpatch.DiffDelete:
			// Red background for deletions
			result.WriteString(`[white:red]` + escapeBrackets(d.Text) + `[-:-:-]`)
		case diffmatchpatch.DiffEqual:
			// Default formatting for unchanged
			result.WriteString(escapeBrackets(d.Text))
		}
	}
	return result.String()
}

type ConsoleLogger struct {
	LogFunc func(msg string)
}

func (cl ConsoleLogger) Info(msg string, args ...any) {
	escaped := escapeBrackets(fmt.Sprintf(msg, args...))
	cl.LogFunc("[INFO] " + escaped)
}

func (cl ConsoleLogger) Debug(msg string, args ...any) {
	escaped := escapeBrackets(fmt.Sprintf(msg, args...))
	cl.LogFunc("[#bdc9c4] " + escaped)
}

func (cl ConsoleLogger) Warning(msg string, args ...any) {
	escaped := escapeBrackets(fmt.Sprintf(msg, args...))
	cl.LogFunc("[orange] " + escaped)
}

func (cl ConsoleLogger) Error(msg string, args ...any) {
	escaped := escapeBrackets(fmt.Sprintf(msg, args...))
	cl.LogFunc("[red] " + escaped)
}

type ConsoleApp struct {
	app            *tview.Application
	watcher        *fsnotify.Watcher
	selectedStep   int
	mutex          sync.Mutex
	execLog        *tview.TextView
	description    *tview.TextView
	stepOutput     *tview.TextView
	steps          *tview.TreeView
	configFilePath string
	profilerData   []apigorowler.StepProfilerData
	stopFn         context.CancelFunc
}

func recoverAndLog(logger ConsoleLogger) {
	if r := recover(); r != nil {
		stack := debug.Stack()
		logger.Error("Recovered from panic: %v\nStack Trace:\n%s", r, string(stack))
	}
}

func NewConsoleApp() *ConsoleApp {
	return &ConsoleApp{
		app:          tview.NewApplication(),
		selectedStep: 0,
		profilerData: make([]apigorowler.StepProfilerData, 0),
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
		inputField.SetLabel("Invalid path. Enter path to configuration file: ")
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

	c.stepOutput = tview.NewTextView()
	c.stepOutput.SetDynamicColors(true)
	c.stepOutput.SetScrollable(true)
	c.stepOutput.SetBorder(true)
	c.stepOutput.SetTitle("Output")

	root := tview.NewTreeNode("").SetSelectable(false)
	c.steps = tview.NewTreeView().SetRoot(root)
	c.steps.SetBorder(true)
	c.steps.SetTitle("Steps")

	c.app.EnableMouse(true)

	c.stepOutput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		x, y := c.stepOutput.GetScrollOffset()
		switch event.Key() {
		case tcell.KeyUp:
			c.stepOutput.ScrollTo(x, y-1)
		case tcell.KeyDown:
			c.stepOutput.ScrollTo(x, y+1)
		case tcell.KeyPgUp:
			c.stepOutput.ScrollTo(x, y-5)
		case tcell.KeyPgDn:
			c.stepOutput.ScrollTo(x, y+5)
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

	focusOrder := []tview.Primitive{c.steps, c.stepOutput}
	currentFocus := 0

	c.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyTAB {
			currentFocus = (currentFocus + 1) % len(focusOrder)
			c.app.SetFocus(focusOrder[currentFocus])
			return nil
		}
		return event
	})

	c.steps.SetSelectedFunc(func(node *tview.TreeNode) {
		if len(node.GetChildren()) > 0 {
			node.SetExpanded(!node.IsExpanded())
		}
	})

	c.steps.SetChangedFunc(c.updateOnChangeStepNode)

	center := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(c.description, 0, 1, false)

	mainFlex := tview.NewFlex().
		AddItem(c.steps, 50, 1, true).
		AddItem(center, 0, 2, false).
		AddItem(c.stepOutput, 0, 3, false)

	execRow := tview.NewFlex().
		AddItem(c.execLog, 0, 1, false).
		AddItem(stopButton, 15, 0, false).
		AddItem(dumpButton, 15, 0, false)

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(execRow, 7, 0, false).
		AddItem(mainFlex, 0, 1, true)

	// Switch UI to main layout (no second Run call!)
	c.app.SetRoot(layout, true).SetFocus(c.steps)

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

func (c *ConsoleApp) updateOnChangeStepNode(node *tview.TreeNode) {
	ref := node.GetReference()
	if ref == nil {
		return
	}

	// You must store the original index or StepProfilerData on the node
	data, ok := ref.(apigorowler.StepProfilerData)
	if !ok {
		return
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.description.ScrollToBeginning()
	c.stepOutput.ScrollToBeginning()

	// Format config
	conf, _ := json.MarshalIndent(data.Config, "", "  ")

	// Build description text
	descriptionText := ""
	descriptionText += fmt.Sprintf("[green]Step Name:[-:-:-:-]%s\n", data.Name)
	descriptionText += fmt.Sprintf("[green]Step Configuration:\n[-:-:-:-]%s\n", escapeBrackets(string(conf)))
	descriptionText += "\n"
	for k, v := range data.Extra {
		descriptionText += fmt.Sprintf("[green]%s:\n[-:-:-:-]%s\n\n", k, v)
	}

	c.description.SetText(descriptionText)
	c.stepOutput.SetText(data.DataString)
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
	c.profilerData = make([]apigorowler.StepProfilerData, 0)
	if c.stopFn != nil {
		c.stopFn()
	}

	go func() {
		logger := ConsoleLogger{
			LogFunc: func(msg string) {
				c.appendLog(msg)
			},
		}
		defer recoverAndLog(logger)

		// accuumulator for stream data
		streamedData := make([]interface{}, 0)

		craw, _, _ := apigorowler.NewApiCrawler(c.configFilePath)
		craw.SetLogger(logger)
		profiler := craw.EnableProfiler()
		defer close(profiler)

		go func() {
			nodeStack := []*tview.TreeNode{c.steps.GetRoot()} // root as initial parent

			for d := range profiler {
				if d.Type == apigorowler.STEP_PROFILER_TYPE_END_SILENT {
					// End nesting — pop the current parent (but never pop root)
					if len(nodeStack) > 1 {
						nodeStack = nodeStack[:len(nodeStack)-1]
					}
					continue
				}
				// Marshal Data into string
				dataString, _ := json.MarshalIndent(d.Data, "", "  ")
				if d.DataBefore != nil {
					dataStringBefore, _ := json.MarshalIndent(d.DataBefore, "", "  ")
					d.Extra["Step Diff with prev"] = (getColoredDiff(string(dataStringBefore), string(dataString)))
				}
				d.DataString = escapeBrackets(string(dataString))

				c.profilerData = append(c.profilerData, d)

				// Create the tree node with reference to the data
				node := tview.NewTreeNode(d.Name).
					SetReference(d).
					SetSelectable(true)

				// Append to current parent node
				currentParent := nodeStack[len(nodeStack)-1]
				currentParent.AddChild(node)
				c.steps.SetCurrentNode(node)
				c.updateOnChangeStepNode(node)

				switch d.Type {
				case apigorowler.STEP_PROFILER_TYPE_START:
					// Start a nested block — push this node as new parent
					nodeStack = append(nodeStack, node)
				case apigorowler.STEP_PROFILER_TYPE_END:
					// End nesting — pop the current parent (but never pop root)
					if len(nodeStack) > 1 {
						nodeStack = nodeStack[:len(nodeStack)-1]
					}
				case apigorowler.STEP_PROFILER_TYPE_NONE:
					// Do nothing, stay at same level
				}
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
					streamedData = append(streamedData, d)
				}
			}()
		}

		err := craw.Run(ctx)

		if err != nil {
			c.appendLog("[red]" + escapeBrackets(err.Error()))
		} else {
			if !craw.Config.Stream {
				res := craw.GetData()
				c.app.QueueUpdateDraw(func() {
					output, _ := json.MarshalIndent(res, "", "   ")
					c.stepOutput.SetText(escapeBrackets(string(output)))
				})
			} else {
				close(craw.GetDataStream())
				// Get the last profiler entry
				lastIndex := len(c.profilerData) - 1
				lastData := c.profilerData[lastIndex]

				// Update its data string
				lastData.Data = streamedData

				outputText := ""
				for _, d := range streamedData {
					output, _ := json.MarshalIndent(d, "", "   ")
					if len(outputText) != 0 {
						outputText += "\n---\n"
					}
					outputText += string(output)
				}
				lastData.DataString = escapeBrackets(outputText)
				c.profilerData[lastIndex] = lastData // reassign (if you're using value semantics)

				// -------- Update the Tree Node --------

				// Assuming you stored node references or can traverse to the last added node:
				var updateLastNode func(node *tview.TreeNode) bool
				updateLastNode = func(node *tview.TreeNode) bool {
					children := node.GetChildren()
					if len(children) == 0 {
						return false
					}
					lastChild := children[len(children)-1]
					if len(lastChild.GetChildren()) == 0 {
						// It's a leaf node, update it
						lastChild.SetReference(lastData)
						c.steps.SetCurrentNode(lastChild)
						c.updateOnChangeStepNode(lastChild)
						return true
					}
					return updateLastNode(lastChild)
				}

				// Start recursive search from the root
				updateLastNode(c.steps.GetRoot())

			}
			c.appendLog("[green]Crawler run completed successfully")
		}
	}()
}

func (c *ConsoleApp) onConfigFileChanged() {
	c.description.SetText("")
	c.stepOutput.SetText("")
	c.steps.GetRoot().ClearChildren()

	data, err := os.ReadFile(c.configFilePath)
	if err != nil {
		log.Printf("Error reading config file: %v", err)
		c.app.QueueUpdateDraw(func() {
			c.execLog.SetText(fmt.Sprintf("[red]Error reading config file: %v", err))
		})
		return
	}

	var cfg apigorowler.Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		c.appendLog("[red]" + escapeBrackets(err.Error()))
		return
	}

	errors := apigorowler.ValidateConfig(cfg)
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

	sanitizeFilename := func(name string) string {
		name = strings.ReplaceAll(name, " ", "_")
		invalidChars := regexp.MustCompile(`[\\/:*?"<>|]`)
		name = invalidChars.ReplaceAllString(name, "")
		return name
	}

	depth := 0

	for i, step := range c.profilerData {
		// Apply prefix
		prefix := strings.Repeat("__", depth)
		prefixedName := prefix + step.Name

		// Marshal step data
		b, err := json.MarshalIndent(step, "", "  ")
		if err != nil {
			c.appendLog(fmt.Sprintf("[red]Failed to marshal step %d: %v", i, err))
			continue
		}

		// Sanitize name and write
		escapedStepName := sanitizeFilename(prefixedName)
		filename := filepath.Join(dumpDir, fmt.Sprintf("%d_%s.json", i, escapedStepName))
		err = os.WriteFile(filename, b, 0644)
		if err != nil {
			c.appendLog(fmt.Sprintf("[red]Failed to write step %d: %v", i, err))
			continue
		}

		// Adjust depth AFTER writing (based on type)
		switch step.Type {
		case apigorowler.STEP_PROFILER_TYPE_START:
			depth++
		case apigorowler.STEP_PROFILER_TYPE_END:
			if depth > 0 {
				depth--
			}
		}
	}

	go func() {
		c.appendLog(fmt.Sprintf("[green]Dumped %d steps to %s", len(c.profilerData), dumpDir))
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
