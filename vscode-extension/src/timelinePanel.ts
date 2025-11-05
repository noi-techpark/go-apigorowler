import * as vscode from 'vscode';
import { StepProfilerData, ProfileEventType } from './stepsTreeProvider';

export class TimelinePanel {
    private static instance: TimelinePanel | undefined;
    private readonly panel: vscode.WebviewPanel;
    private disposables: vscode.Disposable[] = [];
    private events: StepProfilerData[] = [];

    private constructor(panel: vscode.WebviewPanel, extensionUri: vscode.Uri) {
        this.panel = panel;

        this.panel.onDidDispose(() => this.dispose(), null, this.disposables);

        this.panel.webview.onDidReceiveMessage(
            message => {
                switch (message.command) {
                    case 'selectEvent':
                        vscode.commands.executeCommand('apigorowler.showStepDetails', message.event);
                        break;
                }
            },
            null,
            this.disposables
        );
    }

    public static createOrShow(extensionUri: vscode.Uri) {
        // If we already have a panel, show it
        if (TimelinePanel.instance) {
            TimelinePanel.instance.panel.reveal(vscode.ViewColumn.Three, true);
            return TimelinePanel.instance;
        }

        // Otherwise, create a new panel in the bottom area (Three = Terminal/Debug/Output area)
        const panel = vscode.window.createWebviewPanel(
            'apigorowlerTimeline',
            'Execution Timeline',
            { viewColumn: vscode.ViewColumn.Three, preserveFocus: true },
            {
                enableScripts: true,
                retainContextWhenHidden: true
            }
        );

        TimelinePanel.instance = new TimelinePanel(panel, extensionUri);
        return TimelinePanel.instance;
    }

    public static getInstance(): TimelinePanel | undefined {
        return TimelinePanel.instance;
    }

    public addEvent(event: StepProfilerData) {
        this.events.push(event);
        this.refresh();
    }

    public clear() {
        this.events = [];
        this.refresh();
    }

    private refresh() {
        this.panel.webview.html = this.getHtmlContent();
    }

    public dispose() {
        TimelinePanel.instance = undefined;

        this.panel.dispose();

        while (this.disposables.length) {
            const disposable = this.disposables.pop();
            if (disposable) {
                disposable.dispose();
            }
        }
    }

    private getHtmlContent(): string {
        const nonce = getNonce();

        return `<!DOCTYPE html>
        <html lang="en">
        <head>
            <meta charset="UTF-8">
            <meta name="viewport" content="width=device-width, initial-scale=1.0">
            <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-${nonce}';">
            <title>Execution Timeline</title>
            <style>
                body {
                    font-family: var(--vscode-font-family);
                    font-size: var(--vscode-font-size);
                    color: var(--vscode-foreground);
                    background-color: var(--vscode-editor-background);
                    padding: 20px;
                    overflow-x: auto;
                }
                .header {
                    display: flex;
                    justify-content: space-between;
                    align-items: center;
                    margin-bottom: 20px;
                    padding-bottom: 10px;
                    border-bottom: 2px solid var(--vscode-panel-border);
                }
                .controls {
                    display: flex;
                    gap: 10px;
                }
                .btn {
                    background: var(--vscode-button-background);
                    color: var(--vscode-button-foreground);
                    border: none;
                    padding: 5px 15px;
                    cursor: pointer;
                    border-radius: 2px;
                    font-size: 0.9em;
                }
                .btn:hover {
                    background: var(--vscode-button-hoverBackground);
                }
                #timeline {
                    position: relative;
                    min-height: 400px;
                    background: var(--vscode-editor-background);
                }
                .time-axis {
                    position: absolute;
                    top: 0;
                    left: 0;
                    right: 0;
                    height: 30px;
                    border-bottom: 1px solid var(--vscode-panel-border);
                }
                .time-marker {
                    position: absolute;
                    top: 0;
                    bottom: 0;
                    border-left: 1px solid var(--vscode-descriptionForeground);
                    font-size: 10px;
                    padding-left: 5px;
                    color: var(--vscode-descriptionForeground);
                }
                .timeline-events {
                    position: relative;
                    margin-top: 40px;
                }
                .event-bar {
                    position: absolute;
                    height: 24px;
                    border-radius: 3px;
                    cursor: pointer;
                    display: flex;
                    align-items: center;
                    padding: 0 8px;
                    font-size: 11px;
                    white-space: nowrap;
                    overflow: hidden;
                    text-overflow: ellipsis;
                    transition: opacity 0.2s;
                }
                .event-bar:hover {
                    opacity: 0.8;
                    box-shadow: 0 0 10px rgba(255, 255, 255, 0.3);
                }
                .event-instant {
                    position: absolute;
                    width: 3px;
                    height: 24px;
                    cursor: pointer;
                }
                .event-instant:hover {
                    box-shadow: 0 0 10px rgba(255, 255, 255, 0.5);
                }
                /* Event type colors */
                .event-root { background: var(--vscode-charts-purple); }
                .event-request { background: var(--vscode-charts-blue); }
                .event-foreach { background: var(--vscode-charts-orange); }
                .event-page { background: var(--vscode-charts-green); }
                .event-context { background: var(--vscode-charts-yellow); }
                .event-url { background: var(--vscode-charts-blue); }
                .event-http { background: var(--vscode-charts-blue); }
                .event-response { background: var(--vscode-charts-green); }
                .event-transform { background: var(--vscode-charts-yellow); }
                .event-merge { background: var(--vscode-charts-purple); }
                .event-parallel { background: var(--vscode-charts-red); }
                .event-item { background: var(--vscode-charts-orange); }
                .event-pagination { background: var(--vscode-charts-yellow); }
                .event-result { background: var(--vscode-charts-green); }

                .legend {
                    margin-top: 20px;
                    display: flex;
                    flex-wrap: wrap;
                    gap: 15px;
                    font-size: 11px;
                }
                .legend-item {
                    display: flex;
                    align-items: center;
                    gap: 5px;
                }
                .legend-color {
                    width: 16px;
                    height: 16px;
                    border-radius: 2px;
                }
                .empty-state {
                    text-align: center;
                    padding: 60px 20px;
                    color: var(--vscode-descriptionForeground);
                }
            </style>
        </head>
        <body>
            <div class="header">
                <h3>Execution Timeline</h3>
                <div class="controls">
                    <button class="btn" onclick="zoomIn()">🔍 Zoom In</button>
                    <button class="btn" onclick="zoomOut()">🔍 Zoom Out</button>
                    <button class="btn" onclick="resetZoom()">↺ Reset</button>
                </div>
            </div>

            <div id="timeline">
                ${this.events.length === 0 ? this.renderEmptyState() : this.renderTimeline()}
            </div>

            <div class="legend">
                <div class="legend-item">
                    <div class="legend-color event-root"></div>
                    <span>Root</span>
                </div>
                <div class="legend-item">
                    <div class="legend-color event-request"></div>
                    <span>Request Step</span>
                </div>
                <div class="legend-item">
                    <div class="legend-color event-foreach"></div>
                    <span>ForEach Step</span>
                </div>
                <div class="legend-item">
                    <div class="legend-color event-response"></div>
                    <span>Response</span>
                </div>
                <div class="legend-item">
                    <div class="legend-color event-transform"></div>
                    <span>Transform</span>
                </div>
                <div class="legend-item">
                    <div class="legend-color event-merge"></div>
                    <span>Merge</span>
                </div>
                <div class="legend-item">
                    <div class="legend-color event-parallel"></div>
                    <span>Parallel</span>
                </div>
            </div>

            <script nonce="${nonce}">
                const vscode = acquireVsCodeApi();
                let zoomLevel = 1;

                ${this.renderTimelineScript()}

                function zoomIn() {
                    zoomLevel = Math.min(zoomLevel * 1.5, 10);
                    applyZoom();
                }

                function zoomOut() {
                    zoomLevel = Math.max(zoomLevel / 1.5, 0.1);
                    applyZoom();
                }

                function resetZoom() {
                    zoomLevel = 1;
                    applyZoom();
                }

                function applyZoom() {
                    const events = document.querySelectorAll('.event-bar, .event-instant');
                    events.forEach(el => {
                        const left = parseFloat(el.dataset.left || '0');
                        const width = parseFloat(el.dataset.width || '0');
                        el.style.left = (left * zoomLevel) + 'px';
                        if (width > 0) {
                            el.style.width = (width * zoomLevel) + 'px';
                        }
                    });

                    const markers = document.querySelectorAll('.time-marker');
                    markers.forEach(el => {
                        const left = parseFloat(el.dataset.left || '0');
                        el.style.left = (left * zoomLevel) + 'px';
                    });
                }

                function selectEvent(eventId) {
                    vscode.postMessage({
                        command: 'selectEvent',
                        eventId: eventId
                    });
                }
            </script>
        </body>
        </html>`;
    }

    private renderEmptyState(): string {
        return `
            <div class="empty-state">
                <p>No execution events yet.</p>
                <p>Run a crawler to see the execution timeline.</p>
            </div>
        `;
    }

    private renderTimeline(): string {
        if (this.events.length === 0) {
            return '';
        }

        // Find time bounds
        const startTime = Math.min(...this.events.map(e => new Date(e.timestamp).getTime()));
        const endTime = Math.max(...this.events.map(e => {
            const t = new Date(e.timestamp).getTime();
            return e.duration ? t + e.duration : t;
        }));

        const totalDuration = endTime - startTime;
        const pixelsPerMs = totalDuration > 0 ? 800 / totalDuration : 1;

        // Group events by hierarchy depth for vertical positioning
        const eventsByDepth: Map<number, StepProfilerData[]> = new Map();
        const eventDepths: Map<string, number> = new Map();

        // Calculate depth for each event based on parentId
        const calculateDepth = (event: StepProfilerData): number => {
            if (!event.parentId) {
                return 0;
            }
            const cached = eventDepths.get(event.id);
            if (cached !== undefined) {
                return cached;
            }
            const parent = this.events.find(e => e.id === event.parentId);
            if (!parent) {
                return 0;
            }
            const depth = calculateDepth(parent) + 1;
            eventDepths.set(event.id, depth);
            return depth;
        };

        this.events.forEach(event => {
            const depth = calculateDepth(event);
            if (!eventsByDepth.has(depth)) {
                eventsByDepth.set(depth, []);
            }
            eventsByDepth.get(depth)!.push(event);
        });

        // Render time axis
        const numMarkers = 10;
        let timeAxisHtml = '<div class="time-axis">';
        for (let i = 0; i <= numMarkers; i++) {
            const ms = (totalDuration * i) / numMarkers;
            const time = new Date(startTime + ms);
            const left = ms * pixelsPerMs;
            timeAxisHtml += `<div class="time-marker" data-left="${left}" style="left: ${left}px">${time.toLocaleTimeString()}</div>`;
        }
        timeAxisHtml += '</div>';

        // Render events
        let eventsHtml = '<div class="timeline-events">';
        let rowIndex = 0;

        for (const [depth, events] of Array.from(eventsByDepth.entries()).sort((a, b) => a[0] - b[0])) {
            events.forEach(event => {
                const eventStart = new Date(event.timestamp).getTime() - startTime;
                const eventDuration = event.duration || 0;
                const left = eventStart * pixelsPerMs;
                const top = rowIndex * 30;
                const eventClass = this.getEventClass(event.type);
                const label = event.name || 'Event';

                if (eventDuration > 0) {
                    const width = eventDuration * pixelsPerMs;
                    eventsHtml += `
                        <div class="event-bar ${eventClass}"
                             data-left="${left}"
                             data-width="${width}"
                             style="left: ${left}px; top: ${top}px; width: ${width}px"
                             onclick="selectEvent('${event.id}')"
                             title="${label} (${eventDuration}ms)">
                            ${escapeHtml(label)}
                            ${event.workerId !== undefined ? ` [W${event.workerId}]` : ''}
                        </div>
                    `;
                } else {
                    eventsHtml += `
                        <div class="event-instant ${eventClass}"
                             data-left="${left}"
                             style="left: ${left}px; top: ${top}px"
                             onclick="selectEvent('${event.id}')"
                             title="${label}">
                        </div>
                    `;
                }

                rowIndex++;
            });
        }

        eventsHtml += '</div>';

        return timeAxisHtml + eventsHtml;
    }

    private renderTimelineScript(): string {
        return `
            // Timeline initialization
            console.log('Timeline loaded with ${this.events.length} events');
        `;
    }

    private getEventClass(eventType: number): string {
        switch (eventType) {
            case ProfileEventType.EVENT_ROOT_START:
                return 'event-root';
            case ProfileEventType.EVENT_REQUEST_STEP_START:
            case ProfileEventType.EVENT_REQUEST_STEP_END:
                return 'event-request';
            case ProfileEventType.EVENT_FOREACH_STEP_START:
            case ProfileEventType.EVENT_FOREACH_STEP_END:
                return 'event-foreach';
            case ProfileEventType.EVENT_REQUEST_PAGE_START:
            case ProfileEventType.EVENT_REQUEST_PAGE_END:
                return 'event-page';
            case ProfileEventType.EVENT_CONTEXT_SELECTION:
                return 'event-context';
            case ProfileEventType.EVENT_URL_COMPOSITION:
                return 'event-url';
            case ProfileEventType.EVENT_REQUEST_DETAILS:
                return 'event-http';
            case ProfileEventType.EVENT_REQUEST_RESPONSE:
                return 'event-response';
            case ProfileEventType.EVENT_RESPONSE_TRANSFORM:
                return 'event-transform';
            case ProfileEventType.EVENT_CONTEXT_MERGE:
                return 'event-merge';
            case ProfileEventType.EVENT_PARALLELISM_SETUP:
                return 'event-parallel';
            case ProfileEventType.EVENT_ITEM_SELECTION:
                return 'event-item';
            case ProfileEventType.EVENT_PAGINATION_EVAL:
                return 'event-pagination';
            case ProfileEventType.EVENT_RESULT:
            case ProfileEventType.EVENT_STREAM_RESULT:
                return 'event-result';
            default:
                return 'event-request';
        }
    }
}

function getNonce() {
    let text = '';
    const possible = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
    for (let i = 0; i < 32; i++) {
        text += possible.charAt(Math.floor(Math.random() * possible.length));
    }
    return text;
}

function escapeHtml(text: string): string {
    return text
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#039;');
}
