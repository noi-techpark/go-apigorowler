import * as vscode from 'vscode';
import { StepProfilerData, ProfileEventType } from './stepsTreeProvider';

// Store events globally so they persist across view lifecycles
let globalEvents: StepProfilerData[] = [];

export class TimelineViewProvider implements vscode.WebviewViewProvider {
    public static readonly viewType = 'apigorowler.timeline';
    private _view?: vscode.WebviewView;

    constructor(private readonly _extensionUri: vscode.Uri) {}

    public resolveWebviewView(
        webviewView: vscode.WebviewView,
        context: vscode.WebviewViewResolveContext,
        _token: vscode.CancellationToken,
    ) {
        this._view = webviewView;

        webviewView.webview.options = {
            enableScripts: true,
            localResourceRoots: [this._extensionUri]
        };

        webviewView.webview.html = this._getHtmlForWebview(webviewView.webview);

        webviewView.webview.onDidReceiveMessage(message => {
            switch (message.command) {
                case 'selectEvent':
                    vscode.commands.executeCommand('apigorowler.showStepDetails', message.event);
                    break;
            }
        });
    }

    public addEvent(event: StepProfilerData) {
        globalEvents.push(event);
        this.refresh();
    }

    public clear() {
        globalEvents = [];
        this.refresh();
    }

    private refresh() {
        if (this._view) {
            this._view.webview.html = this._getHtmlForWebview(this._view.webview);
        }
    }

    private _getHtmlForWebview(webview: vscode.Webview) {
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
                    padding: 10px;
                    margin: 0;
                    overflow-x: auto;
                }
                .header {
                    display: flex;
                    justify-content: space-between;
                    align-items: center;
                    margin-bottom: 15px;
                    padding-bottom: 8px;
                    border-bottom: 1px solid var(--vscode-panel-border);
                }
                .controls {
                    display: flex;
                    gap: 8px;
                }
                .btn {
                    background: var(--vscode-button-background);
                    color: var(--vscode-button-foreground);
                    border: none;
                    padding: 4px 10px;
                    cursor: pointer;
                    border-radius: 2px;
                    font-size: 0.85em;
                }
                .btn:hover {
                    background: var(--vscode-button-hoverBackground);
                }
                #timeline {
                    position: relative;
                    min-height: 300px;
                    max-height: 70vh;
                    overflow-y: auto;
                    overflow-x: auto;
                    background: var(--vscode-editor-background);
                }
                .time-axis {
                    position: absolute;
                    top: 0;
                    left: 0;
                    right: 0;
                    height: 25px;
                    border-bottom: 1px solid var(--vscode-panel-border);
                }
                .time-marker {
                    position: absolute;
                    top: 0;
                    bottom: 0;
                    border-left: 1px solid var(--vscode-descriptionForeground);
                    font-size: 9px;
                    padding-left: 3px;
                    color: var(--vscode-descriptionForeground);
                }
                .timeline-events {
                    position: relative;
                    margin-top: 30px;
                }
                .event-bar {
                    position: absolute;
                    height: 22px;
                    border-radius: 2px;
                    cursor: pointer;
                    display: flex;
                    align-items: center;
                    padding: 0 6px;
                    font-size: 10px;
                    white-space: nowrap;
                    overflow: hidden;
                    text-overflow: ellipsis;
                    transition: opacity 0.2s;
                }
                .event-bar:hover {
                    opacity: 0.8;
                    box-shadow: 0 0 8px rgba(255, 255, 255, 0.3);
                }
                /* Event type colors */
                .event-root { background: var(--vscode-charts-purple); }
                .event-request { background: var(--vscode-charts-blue); }
                .event-foreach { background: var(--vscode-charts-orange); }
                .event-page { background: var(--vscode-charts-green); }

                .empty-state {
                    text-align: center;
                    padding: 40px 15px;
                    color: var(--vscode-descriptionForeground);
                }
            </style>
        </head>
        <body>
            <div class="header">
                <h4 style="margin: 0;">Timeline</h4>
                <div class="controls">
                    <button class="btn" onclick="zoomIn()">🔍+</button>
                    <button class="btn" onclick="zoomOut()">🔍-</button>
                    <button class="btn" onclick="resetZoom()">↺</button>
                </div>
            </div>

            <div id="timeline">
                ${globalEvents.length === 0 ? this.renderEmptyState() : this.renderTimeline()}
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
                    const events = document.querySelectorAll('.event-bar');
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
        if (globalEvents.length === 0) {
            return '';
        }

        // Build step groups by pairing START and END events
        const stepGroups = this.buildStepGroups();

        if (stepGroups.length === 0) {
            return '<div class="empty-state"><p>No step durations available yet.</p></div>';
        }

        // Find time bounds from step groups
        const startTime = Math.min(...stepGroups.map(s => s.startTime));
        const endTime = Math.max(...stepGroups.map(s => s.endTime));
        const totalDuration = endTime - startTime;
        const pixelsPerMs = totalDuration > 0 ? 800 / totalDuration : 1;

        // Render time axis
        const numMarkers = 10;
        let timeAxisHtml = '<div class="time-axis">';
        for (let i = 0; i <= numMarkers; i++) {
            const ms = (totalDuration * i) / numMarkers;
            const time = new Date(startTime + ms);
            const left = ms * pixelsPerMs;
            timeAxisHtml += `<div class="time-marker" data-left="${left}" style="left: ${left}px">${time.toLocaleTimeString()}.${(ms % 1000).toFixed(0).padStart(3, '0')}</div>`;
        }
        timeAxisHtml += '</div>';

        // Render step groups
        const rowHeight = 26;
        const totalHeight = stepGroups.length * rowHeight + 40;
        let eventsHtml = `<div class="timeline-events" style="min-height: ${totalHeight}px;">`;
        let rowIndex = 0;

        for (const group of stepGroups) {
            const eventStart = group.startTime - startTime;
            const eventDuration = group.duration;
            const left = eventStart * pixelsPerMs;
            const width = eventDuration * pixelsPerMs;
            const top = rowIndex * rowHeight;
            const eventClass = this.getEventClass(group.type);
            const label = group.label;

            eventsHtml += `
                <div class="event-bar ${eventClass}"
                     data-left="${left}"
                     data-width="${width}"
                     style="left: ${left}px; top: ${top}px; width: ${Math.max(width, 2)}px"
                     onclick="selectEvent('${group.id}')"
                     title="${label} (${eventDuration.toFixed(2)}ms)">
                    ${escapeHtml(label)} ${eventDuration.toFixed(0)}ms
                    ${group.workerId !== undefined ? ` [W${group.workerId}]` : ''}
                </div>
            `;

            rowIndex++;
        }

        eventsHtml += '</div>';

        return timeAxisHtml + eventsHtml;
    }

    private buildStepGroups(): Array<{
        id: string;
        label: string;
        type: number;
        startTime: number;
        endTime: number;
        duration: number;
        workerId?: number;
    }> {
        const groups: Array<{
            id: string;
            label: string;
            type: number;
            startTime: number;
            endTime: number;
            duration: number;
            workerId?: number;
        }> = [];

        // Map to track START events waiting for END events
        const startEvents: Map<string, StepProfilerData> = new Map();

        for (const event of globalEvents) {
            const eventType = event.type;

            // Check if this is a START event
            if (eventType === ProfileEventType.EVENT_REQUEST_STEP_START ||
                eventType === ProfileEventType.EVENT_FOREACH_STEP_START ||
                eventType === ProfileEventType.EVENT_REQUEST_PAGE_START) {
                startEvents.set(event.id, event);
            }
            // Check if this is an END event
            else if (eventType === ProfileEventType.EVENT_REQUEST_STEP_END ||
                     eventType === ProfileEventType.EVENT_FOREACH_STEP_END ||
                     eventType === ProfileEventType.EVENT_REQUEST_PAGE_END) {

                // Find corresponding START event
                const startEvent = startEvents.get(event.id);
                if (startEvent) {
                    const startTime = new Date(startEvent.timestamp).getTime();
                    const endTime = new Date(event.timestamp).getTime();
                    const duration = endTime - startTime;

                    let label = startEvent.name || 'Step';
                    if (startEvent.data?.stepName) {
                        label = startEvent.data.stepName;
                    } else if (startEvent.data?.pageNumber) {
                        label = `Page ${startEvent.data.pageNumber}`;
                    }

                    groups.push({
                        id: event.id,
                        label: label,
                        type: startEvent.type,
                        startTime: startTime,
                        endTime: endTime,
                        duration: duration,
                        workerId: startEvent.workerId
                    });

                    startEvents.delete(event.id);
                }
            }
        }

        // Sort by start time
        groups.sort((a, b) => a.startTime - b.startTime);

        return groups;
    }

    private renderTimelineScript(): string {
        return `
            // Timeline initialization
            console.log('Timeline loaded with ${globalEvents.length} events');
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
