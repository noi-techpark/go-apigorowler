import * as vscode from 'vscode';
import { StepProfilerData, ProfileEventType } from './stepsTreeProvider';

export class StepDetailsPanel {
    private static currentPanel: StepDetailsPanel | undefined;
    private readonly panel: vscode.WebviewPanel;
    private disposables: vscode.Disposable[] = [];

    private constructor(panel: vscode.WebviewPanel, extensionUri: vscode.Uri) {
        this.panel = panel;

        // Handle messages from the webview
        this.panel.webview.onDidReceiveMessage(
            message => {
                switch (message.command) {
                    case 'copy':
                        vscode.env.clipboard.writeText(message.text);
                        vscode.window.showInformationMessage('Copied to clipboard');
                        break;
                }
            },
            null,
            this.disposables
        );

        this.panel.onDidDispose(() => this.dispose(), null, this.disposables);
    }

    public static createOrShow(extensionUri: vscode.Uri, data: StepProfilerData) {
        const column = vscode.ViewColumn.Two;

        // If we already have a panel, show it
        if (StepDetailsPanel.currentPanel) {
            StepDetailsPanel.currentPanel.panel.reveal(column);
            StepDetailsPanel.currentPanel.update(data);
            return;
        }

        // Otherwise, create a new panel
        const panel = vscode.window.createWebviewPanel(
            'apigorowlerStepDetails',
            'Step Details',
            column,
            {
                enableScripts: true,
                retainContextWhenHidden: true
            }
        );

        StepDetailsPanel.currentPanel = new StepDetailsPanel(panel, extensionUri);
        StepDetailsPanel.currentPanel.update(data);
    }

    public update(data: StepProfilerData) {
        this.panel.title = `Step Details: ${data.name || 'Unknown'}`;
        this.panel.webview.html = this.getHtmlForStep(data);
    }

    public dispose() {
        StepDetailsPanel.currentPanel = undefined;

        this.panel.dispose();

        while (this.disposables.length) {
            const disposable = this.disposables.pop();
            if (disposable) {
                disposable.dispose();
            }
        }
    }

    private getHtmlForStep(data: StepProfilerData): string {
        const nonce = getNonce();

        return `<!DOCTYPE html>
        <html lang="en">
        <head>
            <meta charset="UTF-8">
            <meta name="viewport" content="width=device-width, initial-scale=1.0">
            <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-${nonce}';">
            <title>Step Details</title>
            <style>
                body {
                    font-family: var(--vscode-font-family);
                    font-size: var(--vscode-font-size);
                    color: var(--vscode-foreground);
                    background-color: var(--vscode-editor-background);
                    padding: 20px;
                    line-height: 1.6;
                }
                .header {
                    border-bottom: 2px solid var(--vscode-panel-border);
                    padding-bottom: 15px;
                    margin-bottom: 20px;
                }
                .header h2 {
                    margin: 0 0 10px 0;
                    color: var(--vscode-titleBar-activeForeground);
                }
                .timestamp {
                    color: var(--vscode-descriptionForeground);
                    font-size: 0.9em;
                }
                .section {
                    margin-bottom: 20px;
                }
                .section-header {
                    display: flex;
                    justify-content: space-between;
                    align-items: center;
                    margin-bottom: 8px;
                }
                .section-title {
                    font-weight: 600;
                    color: var(--vscode-textLink-foreground);
                }
                .actions {
                    display: flex;
                    gap: 10px;
                }
                .btn {
                    background: var(--vscode-button-background);
                    color: var(--vscode-button-foreground);
                    border: none;
                    padding: 4px 12px;
                    cursor: pointer;
                    border-radius: 2px;
                    font-size: 0.85em;
                }
                .btn:hover {
                    background: var(--vscode-button-hoverBackground);
                }
                .code-block {
                    background: var(--vscode-textCodeBlock-background);
                    border: 1px solid var(--vscode-panel-border);
                    border-radius: 3px;
                    padding: 12px;
                    overflow-x: auto;
                    font-family: var(--vscode-editor-font-family);
                    font-size: 0.9em;
                    white-space: pre-wrap;
                    word-wrap: break-word;
                }
                .side-by-side {
                    display: grid;
                    grid-template-columns: 1fr 1fr;
                    gap: 15px;
                    margin-top: 10px;
                }
                .diff-highlight {
                    background: var(--vscode-diffEditor-insertedTextBackground);
                    padding: 2px 4px;
                    border-radius: 2px;
                }
                .status-badge {
                    display: inline-block;
                    padding: 4px 12px;
                    border-radius: 12px;
                    font-weight: 600;
                    font-size: 0.9em;
                }
                .status-success {
                    background: var(--vscode-testing-iconPassed);
                    color: white;
                }
                .status-error {
                    background: var(--vscode-testing-iconFailed);
                    color: white;
                }
                .context-path {
                    font-family: monospace;
                    background: var(--vscode-badge-background);
                    padding: 8px 12px;
                    border-radius: 4px;
                    display: inline-block;
                }
                .info-box {
                    background: var(--vscode-inputValidation-infoBackground);
                    border-left: 4px solid var(--vscode-inputValidation-infoBorder);
                    padding: 12px;
                    margin-top: 10px;
                    border-radius: 3px;
                }
                details {
                    margin: 10px 0;
                }
                summary {
                    cursor: pointer;
                    font-weight: 600;
                    padding: 8px;
                    background: var(--vscode-list-hoverBackground);
                    border-radius: 3px;
                }
                summary:hover {
                    background: var(--vscode-list-activeSelectionBackground);
                }
            </style>
        </head>
        <body>
            ${this.renderStepDetails(data)}
            <script nonce="${nonce}">
                const vscode = acquireVsCodeApi();

                function copyToClipboard(text) {
                    vscode.postMessage({
                        command: 'copy',
                        text: text
                    });
                }
            </script>
        </body>
        </html>`;
    }

    private renderStepDetails(data: StepProfilerData): string {
        switch (data.type) {
            case ProfileEventType.EVENT_ROOT_START:
                return this.renderRootStart(data);
            case ProfileEventType.EVENT_REQUEST_STEP_START:
                return this.renderRequestStep(data);
            case ProfileEventType.EVENT_FOREACH_STEP_START:
                return this.renderForEachStep(data);
            case ProfileEventType.EVENT_CONTEXT_SELECTION:
                return this.renderContextSelection(data);
            case ProfileEventType.EVENT_PAGINATION_EVAL:
                return this.renderPaginationEval(data);
            case ProfileEventType.EVENT_URL_COMPOSITION:
                return this.renderUrlComposition(data);
            case ProfileEventType.EVENT_REQUEST_DETAILS:
                return this.renderRequestDetails(data);
            case ProfileEventType.EVENT_REQUEST_RESPONSE:
                return this.renderRequestResponse(data);
            case ProfileEventType.EVENT_RESPONSE_TRANSFORM:
                return this.renderResponseTransform(data);
            case ProfileEventType.EVENT_CONTEXT_MERGE:
                return this.renderContextMerge(data);
            case ProfileEventType.EVENT_PARALLELISM_SETUP:
                return this.renderParallelismSetup(data);
            case ProfileEventType.EVENT_ITEM_SELECTION:
                return this.renderItemSelection(data);
            case ProfileEventType.EVENT_RESULT:
            case ProfileEventType.EVENT_STREAM_RESULT:
                return this.renderResult(data);
            default:
                return this.renderGeneric(data);
        }
    }

    private renderRootStart(data: StepProfilerData): string {
        const contextMap = JSON.stringify(data.data?.contextMap || {}, null, 2);
        const config = JSON.stringify(data.data?.config || {}, null, 2);

        return `
            <div class="header">
                <h2>📦 Root Start</h2>
                <div class="timestamp">⏱️  ${new Date(data.timestamp).toLocaleString()}</div>
            </div>

            <div class="section">
                <div class="section-header">
                    <div class="section-title">Initial Context Map</div>
                    <div class="actions">
                        <button class="btn" onclick="copyToClipboard(\`${escapeHtml(contextMap)}\`)">📋 Copy</button>
                    </div>
                </div>
                <details open>
                    <summary>Context Map</summary>
                    <div class="code-block">${escapeHtml(contextMap)}</div>
                </details>
            </div>

            <div class="section">
                <div class="section-header">
                    <div class="section-title">Configuration</div>
                    <div class="actions">
                        <button class="btn" onclick="copyToClipboard(\`${escapeHtml(config)}\`)">📋 Copy</button>
                    </div>
                </div>
                <details>
                    <summary>Crawler Configuration</summary>
                    <div class="code-block">${escapeHtml(config)}</div>
                </details>
            </div>
        `;
    }

    private renderRequestStep(data: StepProfilerData): string {
        const stepConfig = JSON.stringify(data.data?.stepConfig || {}, null, 2);
        const duration = data.duration ? `${data.duration}ms (${(data.duration / 1000).toFixed(3)}s)` : 'In progress...';

        return `
            <div class="header">
                <h2>🔄 Request Step: ${data.name}</h2>
                <div class="timestamp">⏱️  Started: ${new Date(data.timestamp).toLocaleString()}</div>
                ${data.duration ? `<div class="timestamp">⏱️  Duration: ${duration}</div>` : ''}
            </div>

            <div class="section">
                <div class="section-header">
                    <div class="section-title">Step Configuration</div>
                    <div class="actions">
                        <button class="btn" onclick="copyToClipboard(\`${escapeHtml(stepConfig)}\`)">📋 Copy</button>
                    </div>
                </div>
                <details open>
                    <summary>Configuration</summary>
                    <div class="code-block">${escapeHtml(stepConfig)}</div>
                </details>
            </div>
        `;
    }

    private renderForEachStep(data: StepProfilerData): string {
        const stepConfig = JSON.stringify(data.data?.stepConfig || {}, null, 2);
        const duration = data.duration ? `${data.duration}ms` : 'In progress...';

        return `
            <div class="header">
                <h2>🔁 ForEach Step: ${data.name}</h2>
                <div class="timestamp">⏱️  Started: ${new Date(data.timestamp).toLocaleString()}</div>
                ${data.duration ? `<div class="timestamp">⏱️  Duration: ${duration}</div>` : ''}
            </div>

            <div class="section">
                <div class="section-header">
                    <div class="section-title">Step Configuration</div>
                    <div class="actions">
                        <button class="btn" onclick="copyToClipboard(\`${escapeHtml(stepConfig)}\`)">📋 Copy</button>
                    </div>
                </div>
                <details open>
                    <summary>Configuration</summary>
                    <div class="code-block">${escapeHtml(stepConfig)}</div>
                </details>
            </div>
        `;
    }

    private renderContextSelection(data: StepProfilerData): string {
        const contextPath = data.data?.contextPath || '';
        const currentKey = data.data?.currentContextKey || '';
        const contextData = JSON.stringify(data.data?.currentContextData || {}, null, 2);
        const fullContextMap = JSON.stringify(data.data?.fullContextMap || {}, null, 2);

        return `
            <div class="header">
                <h2>📍 Context Selection</h2>
                <div class="timestamp">⏱️  ${new Date(data.timestamp).toLocaleString()}</div>
            </div>

            <div class="section">
                <div class="section-title">Context Path</div>
                <div class="context-path">${contextPath}</div>
            </div>

            <div class="section">
                <div class="section-title">Current Context Key</div>
                <div class="code-block">"${currentKey}"</div>
            </div>

            <div class="section">
                <div class="section-header">
                    <div class="section-title">Current Context Data</div>
                    <div class="actions">
                        <button class="btn" onclick="copyToClipboard(\`${escapeHtml(contextData)}\`)">📋 Copy</button>
                    </div>
                </div>
                <details open>
                    <summary>Context Data</summary>
                    <div class="code-block">${escapeHtml(contextData)}</div>
                </details>
            </div>

            <div class="section">
                <div class="section-header">
                    <div class="section-title">Full Context Map (after selection)</div>
                    <div class="actions">
                        <button class="btn" onclick="copyToClipboard(\`${escapeHtml(fullContextMap)}\`)">📋 Copy</button>
                    </div>
                </div>
                <details>
                    <summary>Click to show updated context map</summary>
                    <div class="code-block">${escapeHtml(fullContextMap)}</div>
                </details>
            </div>
        `;
    }

    private renderPaginationEval(data: StepProfilerData): string {
        const pageNumber = data.data?.pageNumber || 0;
        const paginationConfig = JSON.stringify(data.data?.paginationConfig || {}, null, 2);
        const previousResponse = JSON.stringify(data.data?.previousResponse || {}, null, 2);
        const beforeState = data.data?.previousState || {};
        const afterState = data.data?.afterState || {};

        return `
            <div class="header">
                <h2>⚙️  Pagination Evaluation (Page ${pageNumber})</h2>
                <div class="timestamp">⏱️  ${new Date(data.timestamp).toLocaleString()}</div>
                <div class="timestamp">📄 Page Number: ${pageNumber}</div>
            </div>

            <div class="section">
                <div class="section-header">
                    <div class="section-title">Pagination Configuration</div>
                    <div class="actions">
                        <button class="btn" onclick="copyToClipboard(\`${escapeHtml(paginationConfig)}\`)">📋 Copy</button>
                    </div>
                </div>
                <details>
                    <summary>Configuration</summary>
                    <div class="code-block">${escapeHtml(paginationConfig)}</div>
                </details>
            </div>

            <div class="section">
                <div class="section-header">
                    <div class="section-title">Previous Response (used for extraction)</div>
                    <div class="actions">
                        <button class="btn" onclick="copyToClipboard(\`${escapeHtml(previousResponse)}\`)">📋 Copy</button>
                    </div>
                </div>
                <details>
                    <summary>Click to show response body and headers</summary>
                    <div class="code-block">${escapeHtml(previousResponse)}</div>
                </details>
            </div>

            <div class="section">
                <div class="section-title">State Comparison</div>
                <div class="side-by-side">
                    <div>
                        <h4>Before State</h4>
                        <div class="code-block">${escapeHtml(JSON.stringify(beforeState, null, 2))}</div>
                    </div>
                    <div>
                        <h4>After State</h4>
                        <div class="code-block">${escapeHtml(JSON.stringify(afterState, null, 2))}</div>
                    </div>
                </div>
            </div>
        `;
    }

    private renderUrlComposition(data: StepProfilerData): string {
        const urlTemplate = data.data?.urlTemplate || '';
        const templateContext = JSON.stringify(data.data?.goTemplateContext || {}, null, 2);
        const paginationState = JSON.stringify(data.data?.paginationState || {}, null, 2);
        const resultUrl = data.data?.resultUrl || '';
        const resultHeaders = JSON.stringify(data.data?.resultHeaders || {}, null, 2);
        const resultBody = JSON.stringify(data.data?.resultBody || {}, null, 2);

        return `
            <div class="header">
                <h2>🔗 URL Composition</h2>
                <div class="timestamp">⏱️  ${new Date(data.timestamp).toLocaleString()}</div>
            </div>

            <div class="section">
                <div class="section-title">URL Template</div>
                <div class="code-block">${escapeHtml(urlTemplate)}</div>
            </div>

            <div class="section">
                <div class="section-header">
                    <div class="section-title">Template Context</div>
                </div>
                <details>
                    <summary>Go Template Variables</summary>
                    <div class="code-block">${escapeHtml(templateContext)}</div>
                </details>
            </div>

            <div class="section">
                <div class="section-header">
                    <div class="section-title">Pagination State</div>
                </div>
                <details>
                    <summary>Pagination Parameters</summary>
                    <div class="code-block">${escapeHtml(paginationState)}</div>
                </details>
            </div>

            <div class="section">
                <div class="section-header">
                    <div class="section-title">✅ Resulting URL</div>
                    <div class="actions">
                        <button class="btn" onclick="copyToClipboard(\`${escapeHtml(resultUrl)}\`)">📋 Copy</button>
                    </div>
                </div>
                <div class="code-block">${escapeHtml(resultUrl)}</div>
            </div>

            <div class="section">
                <details>
                    <summary>Resulting Headers</summary>
                    <div class="code-block">${escapeHtml(resultHeaders)}</div>
                </details>
            </div>

            <div class="section">
                <details>
                    <summary>Resulting Body</summary>
                    <div class="code-block">${escapeHtml(resultBody)}</div>
                </details>
            </div>
        `;
    }

    private renderRequestDetails(data: StepProfilerData): string {
        const method = data.data?.method || '';
        const url = data.data?.url || '';
        const curl = data.data?.curl || '';
        const headers = JSON.stringify(data.data?.headers || {}, null, 2);
        const body = JSON.stringify(data.data?.body || {}, null, 2);

        return `
            <div class="header">
                <h2>📤 Request Details</h2>
                <div class="timestamp">⏱️  ${new Date(data.timestamp).toLocaleString()}</div>
            </div>

            <div class="section">
                <div class="section-title">Method & URL</div>
                <div class="code-block">${method} ${escapeHtml(url)}</div>
            </div>

            <div class="section">
                <div class="section-header">
                    <div class="section-title">💻 curl Command</div>
                    <div class="actions">
                        <button class="btn" onclick="copyToClipboard(\`${escapeHtml(curl)}\`)">📋 Copy</button>
                    </div>
                </div>
                <div class="code-block">${escapeHtml(curl)}</div>
            </div>

            <div class="section">
                <details open>
                    <summary>Request Headers</summary>
                    <div class="code-block">${escapeHtml(headers)}</div>
                </details>
            </div>

            <div class="section">
                <details>
                    <summary>Request Body</summary>
                    <div class="code-block">${body ? escapeHtml(body) : '(none)'}</div>
                </details>
            </div>
        `;
    }

    private renderRequestResponse(data: StepProfilerData): string {
        const statusCode = data.data?.statusCode || 0;
        const statusClass = statusCode >= 200 && statusCode < 300 ? 'status-success' : 'status-error';
        const headers = JSON.stringify(data.data?.headers || {}, null, 2);
        const body = JSON.stringify(data.data?.body || {}, null, 2);
        const responseSize = data.data?.responseSize || 0;
        const duration = data.data?.durationMs || 0;

        return `
            <div class="header">
                <h2>📥 Response</h2>
                <div class="timestamp">⏱️  ${new Date(data.timestamp).toLocaleString()}</div>
                <div class="timestamp">⏱️  Duration: ${duration}ms</div>
            </div>

            <div class="section">
                <div class="section-title">Status Code</div>
                <span class="status-badge ${statusClass}">${statusCode} ${getStatusText(statusCode)}</span>
            </div>

            <div class="section">
                <div class="section-title">Response Info</div>
                <div>📦 Size: ${formatBytes(responseSize)}</div>
                <div>⏱️  Time: ${duration} ms</div>
            </div>

            <div class="section">
                <details>
                    <summary>Response Headers</summary>
                    <div class="code-block">${escapeHtml(headers)}</div>
                </details>
            </div>

            <div class="section">
                <div class="section-header">
                    <div class="section-title">Response Body</div>
                    <div class="actions">
                        <button class="btn" onclick="copyToClipboard(\`${escapeHtml(body)}\`)">📋 Copy</button>
                    </div>
                </div>
                <details open>
                    <summary>Body Content</summary>
                    <div class="code-block">${escapeHtml(body)}</div>
                </details>
            </div>
        `;
    }

    private renderResponseTransform(data: StepProfilerData): string {
        const transformRule = data.data?.transformRule || '';
        const before = JSON.stringify(data.data?.beforeResponse || {}, null, 2);
        const after = JSON.stringify(data.data?.afterResponse || {}, null, 2);

        return `
            <div class="header">
                <h2>⚡ Response Transform</h2>
                <div class="timestamp">⏱️  ${new Date(data.timestamp).toLocaleString()}</div>
            </div>

            <div class="section">
                <div class="section-title">Transform Rule</div>
                <div class="code-block">${escapeHtml(transformRule)}</div>
            </div>

            <div class="section">
                <div class="section-title">Before/After Comparison</div>
                <div class="side-by-side">
                    <div>
                        <h4>Before Transform</h4>
                        <div class="code-block">${escapeHtml(before)}</div>
                    </div>
                    <div>
                        <h4>After Transform</h4>
                        <div class="code-block">${escapeHtml(after)}</div>
                    </div>
                </div>
            </div>
        `;
    }

    private renderContextMerge(data: StepProfilerData): string {
        const currentKey = data.data?.currentContextKey || '';
        const targetKey = data.data?.targetContextKey || '';
        const mergeRule = data.data?.mergeRule || '';
        const before = JSON.stringify(data.data?.targetContextBefore || {}, null, 2);
        const after = JSON.stringify(data.data?.targetContextAfter || {}, null, 2);
        const fullContextMap = JSON.stringify(data.data?.fullContextMap || {}, null, 2);

        return `
            <div class="header">
                <h2>🔀 Context Merge</h2>
                <div class="timestamp">⏱️  ${new Date(data.timestamp).toLocaleString()}</div>
            </div>

            <div class="section">
                <div class="section-title">Merge Direction</div>
                <div>From: <span class="context-path">${currentKey}</span></div>
                <div>To: <span class="context-path">${targetKey}</span></div>
            </div>

            <div class="section">
                <div class="section-title">Merge Rule</div>
                <div class="code-block">${escapeHtml(mergeRule)}</div>
            </div>

            <div class="section">
                <div class="section-title">Target Context Comparison</div>
                <div class="side-by-side">
                    <div>
                        <h4>Before Merge</h4>
                        <div class="code-block">${escapeHtml(before)}</div>
                    </div>
                    <div>
                        <h4>After Merge</h4>
                        <div class="code-block">${escapeHtml(after)}</div>
                    </div>
                </div>
            </div>

            <div class="section">
                <details>
                    <summary>Full Context Map (after merge)</summary>
                    <div class="code-block">${escapeHtml(fullContextMap)}</div>
                </details>
            </div>
        `;
    }

    private renderParallelismSetup(data: StepProfilerData): string {
        const maxConcurrency = data.data?.maxConcurrency || 0;
        const workerPoolId = data.data?.workerPoolId || '';
        const workerIds = data.data?.workerIds || [];
        const rateLimit = data.data?.rateLimit || null;
        const burst = data.data?.burst || 1;

        return `
            <div class="header">
                <h2>⚡ Parallelism Setup</h2>
                <div class="timestamp">⏱️  ${new Date(data.timestamp).toLocaleString()}</div>
            </div>

            <div class="section">
                <div class="section-title">Worker Pool Configuration</div>
                <div>🏊 Pool ID: <code>${workerPoolId}</code></div>
                <div>👷 Max Concurrency: <strong>${maxConcurrency}</strong> workers</div>
                <div>🔢 Worker IDs: ${workerIds.join(', ')}</div>
            </div>

            ${rateLimit ? `
            <div class="section">
                <div class="section-title">Rate Limiting</div>
                <div>🚦 Rate Limit: ${rateLimit} requests/second</div>
                <div>💥 Burst: ${burst}</div>
            </div>
            ` : ''}
        `;
    }

    private renderItemSelection(data: StepProfilerData): string {
        const iterationIndex = data.data?.iterationIndex ?? 0;
        const itemValue = JSON.stringify(data.data?.itemValue || {}, null, 2);
        const currentKey = data.data?.currentContextKey || '';
        const contextData = JSON.stringify(data.data?.currentContextData || {}, null, 2);

        return `
            <div class="header">
                <h2>📦 Item Selection</h2>
                <div class="timestamp">⏱️  ${new Date(data.timestamp).toLocaleString()}</div>
                ${data.workerId !== undefined ? `<div class="timestamp">👷 Worker: ${data.workerId}</div>` : ''}
            </div>

            <div class="section">
                <div class="section-title">Iteration Index</div>
                <div><strong>${iterationIndex}</strong></div>
            </div>

            <div class="section">
                <div class="section-title">Current Context Key</div>
                <div class="context-path">${currentKey}</div>
            </div>

            <div class="section">
                <div class="section-header">
                    <div class="section-title">Item Value</div>
                    <div class="actions">
                        <button class="btn" onclick="copyToClipboard(\`${escapeHtml(itemValue)}\`)">📋 Copy</button>
                    </div>
                </div>
                <details open>
                    <summary>Value</summary>
                    <div class="code-block">${escapeHtml(itemValue)}</div>
                </details>
            </div>

            <div class="section">
                <div class="section-header">
                    <div class="section-title">Current Context Data</div>
                    <div class="actions">
                        <button class="btn" onclick="copyToClipboard(\`${escapeHtml(contextData)}\`)">📋 Copy</button>
                    </div>
                </div>
                <details open>
                    <summary>Context</summary>
                    <div class="code-block">${escapeHtml(contextData)}</div>
                </details>
            </div>
        `;
    }

    private renderResult(data: StepProfilerData): string {
        const result = JSON.stringify(data.data?.result || data.data?.entity || {}, null, 2);
        const index = data.data?.index;

        return `
            <div class="header">
                <h2>✅ ${data.type === ProfileEventType.EVENT_STREAM_RESULT ? `Stream Result ${index ?? ''}` : 'Final Result'}</h2>
                <div class="timestamp">⏱️  ${new Date(data.timestamp).toLocaleString()}</div>
            </div>

            <div class="section">
                <div class="section-header">
                    <div class="section-title">Result Data</div>
                    <div class="actions">
                        <button class="btn" onclick="copyToClipboard(\`${escapeHtml(result)}\`)">📋 Copy</button>
                    </div>
                </div>
                <details open>
                    <summary>Data</summary>
                    <div class="code-block">${escapeHtml(result)}</div>
                </details>
            </div>
        `;
    }

    private renderGeneric(data: StepProfilerData): string {
        const dataStr = JSON.stringify(data.data || {}, null, 2);

        return `
            <div class="header">
                <h2>${data.name || 'Step'}</h2>
                <div class="timestamp">⏱️  ${new Date(data.timestamp).toLocaleString()}</div>
            </div>

            <div class="section">
                <div class="section-header">
                    <div class="section-title">Data</div>
                    <div class="actions">
                        <button class="btn" onclick="copyToClipboard(\`${escapeHtml(dataStr)}\`)">📋 Copy</button>
                    </div>
                </div>
                <details open>
                    <summary>Details</summary>
                    <div class="code-block">${escapeHtml(dataStr)}</div>
                </details>
            </div>
        `;
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
        .replace(/'/g, '&#039;')
        .replace(/`/g, '&#096;');
}

function getStatusText(code: number): string {
    const codes: Record<number, string> = {
        200: 'OK',
        201: 'Created',
        204: 'No Content',
        400: 'Bad Request',
        401: 'Unauthorized',
        403: 'Forbidden',
        404: 'Not Found',
        500: 'Internal Server Error',
        502: 'Bad Gateway',
        503: 'Service Unavailable'
    };
    return codes[code] || '';
}

function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return Math.round((bytes / Math.pow(k, i)) * 100) / 100 + ' ' + sizes[i];
}
