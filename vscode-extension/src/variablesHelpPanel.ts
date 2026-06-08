// SPDX-FileCopyrightText: 2024 NOI Techpark <digital@noi.bz.it>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

import * as vscode from 'vscode';
import { getNonce } from './webviewUtils';

/**
 * Panel that explains the two kinds of variables Silky supports and how to use
 * each: Runtime Variables (passed via -vars / the Runtime Variables sidebar) and
 * Environment Variables (injected into the crawler process / the Environment
 * Variables sidebar). Shown side by side so the difference is unambiguous.
 */
export class VariablesHelpPanel {
    public static currentPanel: VariablesHelpPanel | undefined;
    private static readonly viewType = 'silkyVariablesHelp';
    private readonly panel: vscode.WebviewPanel;

    public static createOrShow(extensionUri: vscode.Uri) {
        const column = vscode.ViewColumn.Beside;

        if (VariablesHelpPanel.currentPanel) {
            VariablesHelpPanel.currentPanel.panel.reveal(column);
            return;
        }

        const panel = vscode.window.createWebviewPanel(
            VariablesHelpPanel.viewType,
            'Silky Variables Help',
            column,
            {
                enableScripts: false,
                enableFindWidget: true,
                retainContextWhenHidden: true
            }
        );

        VariablesHelpPanel.currentPanel = new VariablesHelpPanel(panel, extensionUri);
    }

    private constructor(panel: vscode.WebviewPanel, extensionUri: vscode.Uri) {
        this.panel = panel;
        this.panel.webview.html = this.getHtml(panel.webview, extensionUri);
        this.panel.onDidDispose(() => {
            VariablesHelpPanel.currentPanel = undefined;
        });
    }

    private getHtml(webview: vscode.Webview, _extensionUri: vscode.Uri): string {
        const nonce = getNonce();

        return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src ${webview.cspSource} 'unsafe-inline';">
    <title>Silky Variables Help</title>
    <style nonce="${nonce}">
        body {
            font-family: var(--vscode-font-family);
            font-size: var(--vscode-font-size);
            color: var(--vscode-foreground);
            padding: 16px 24px;
            line-height: 1.5;
        }
        h1 { font-size: 1.6em; margin-bottom: 4px; }
        .subtitle { color: var(--vscode-descriptionForeground); margin-bottom: 20px; }
        .cols { display: flex; gap: 24px; flex-wrap: wrap; }
        .card {
            flex: 1 1 320px;
            border: 1px solid var(--vscode-panel-border);
            border-radius: 6px;
            padding: 14px 18px;
        }
        .card h2 {
            font-size: 1.2em;
            margin-top: 0;
            color: var(--vscode-textLink-foreground);
            display: flex;
            align-items: center;
            gap: 8px;
        }
        .tag {
            font-size: 0.75em;
            font-weight: 600;
            padding: 1px 7px;
            border-radius: 10px;
            background: var(--vscode-badge-background);
            color: var(--vscode-badge-foreground);
        }
        h3 { font-size: 0.95em; margin: 16px 0 4px; color: var(--vscode-textLink-activeForeground); }
        code {
            font-family: var(--vscode-editor-font-family);
            font-size: 0.92em;
            background: var(--vscode-textCodeBlock-background);
            padding: 1px 5px;
            border-radius: 3px;
        }
        pre {
            background: var(--vscode-textCodeBlock-background);
            padding: 10px 12px;
            border-radius: 4px;
            overflow-x: auto;
            font-family: var(--vscode-editor-font-family);
            font-size: 0.9em;
        }
        table { border-collapse: collapse; width: 100%; margin-top: 16px; }
        th, td { text-align: left; padding: 6px 10px; border-bottom: 1px solid var(--vscode-panel-border); vertical-align: top; }
        th { font-weight: 600; color: var(--vscode-textLink-foreground); }
        ul { padding-left: 18px; margin: 4px 0; }
    </style>
</head>
<body>
    <h1>Variables in Silky</h1>
    <p class="subtitle">Silky has <strong>two</strong> kinds of variables. They are set in their own sidebar
    accordion (left, under the Silky activity-bar icon) and referenced with different syntax. Both are
    session-only &mdash; they reset when the window reloads.</p>

    <div class="cols">
        <div class="card">
            <h2>Runtime Variables <span class="tag">-vars</span></h2>
            <p>Dynamic values passed <em>into the crawl context</em> at run time (API keys, search terms,
            limits). They become part of the root context.</p>

            <h3>Set</h3>
            <p>Sidebar &rarr; <strong>Runtime Variables</strong> &rarr; <code>+</code> (Add Variable).
            Values are parsed as JSON, so you can store strings, numbers, booleans, arrays, or objects.</p>

            <h3>Reference</h3>
            <ul>
                <li>Go templates (url, headers, body 1st pass): <code>{{ .apiKey }}</code></li>
                <li>jq (body, resultTransformer, merge rules): <code>.apiKey</code> or <code>$ctx.apiKey</code></li>
            </ul>

            <h3>Example</h3>
            <pre>// Runtime Variables panel
apiKey  = "secret123"
limit   = 50

// in YAML
url: "https://api/x?key={{ .apiKey }}&n={{ .limit }}"
body: '{maxResults: $ctx.limit}'</pre>

            <p><strong>Equivalent CLI:</strong> <code>silky -config c.yaml -vars '{"apiKey":"secret123","limit":50}'</code></p>
        </div>

        <div class="card">
            <h2>Environment Variables <span class="tag">\${VAR}</span></h2>
            <p>Process-level values expanded in the <em>raw YAML text</em> before parsing. Good for host
            config and secrets you keep out of the file.</p>

            <h3>Set</h3>
            <p>Sidebar &rarr; <strong>Environment Variables</strong> &rarr; <code>+</code> (Add Variable).
            Values are plain strings and are injected into the crawler process &mdash; you no longer need to
            <code>export</code> them in a shell before launching VS&nbsp;Code.</p>

            <h3>Reference</h3>
            <ul>
                <li>Anywhere in the YAML: <code>\${VAR}</code></li>
                <li>With a fallback: <code>\${VAR:-default}</code></li>
            </ul>

            <h3>Example</h3>
            <pre>// Environment Variables panel
API_HOST = "api.example.com"
API_TOKEN = "abc123"

// in YAML
url: "https://\${API_HOST}/v1/items"
headers:
  Authorization: "Bearer \${API_TOKEN}"
  X-Env: "\${STAGE:-prod}"</pre>

            <p>An unset <code>\${VAR}</code> with no default expands to an empty string.</p>
        </div>
    </div>

    <table>
        <tr><th>&nbsp;</th><th>Runtime Variables</th><th>Environment Variables</th></tr>
        <tr><td>Sidebar panel</td><td>Runtime Variables</td><td>Environment Variables</td></tr>
        <tr><td>Value types</td><td>Any JSON (string, number, object, array)</td><td>Strings only</td></tr>
        <tr><td>Reference syntax</td><td><code>{{ .name }}</code> / <code>$ctx.name</code></td><td><code>\${NAME}</code> / <code>\${NAME:-default}</code></td></tr>
        <tr><td>Expanded</td><td>In template &amp; jq evaluation (context)</td><td>In raw YAML text, before parsing</td></tr>
        <tr><td>Delivered as</td><td><code>-vars</code> JSON flag</td><td>Process environment</td></tr>
        <tr><td>Lifetime</td><td>Session-only</td><td>Session-only</td></tr>
    </table>
</body>
</html>`;
    }
}
