// SPDX-FileCopyrightText: 2024 NOI Techpark <digital@noi.bz.it>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

import * as vscode from 'vscode';
import { getNonce } from './webviewUtils';

/**
 * Panel that shows available template and jq functions reference.
 */
export class FunctionsReferencePanel {
    public static currentPanel: FunctionsReferencePanel | undefined;
    private static readonly viewType = 'silkyFunctionsReference';
    private readonly panel: vscode.WebviewPanel;

    public static createOrShow(extensionUri: vscode.Uri) {
        const column = vscode.ViewColumn.Beside;

        if (FunctionsReferencePanel.currentPanel) {
            FunctionsReferencePanel.currentPanel.panel.reveal(column);
            return;
        }

        const panel = vscode.window.createWebviewPanel(
            FunctionsReferencePanel.viewType,
            'Silky Functions Reference',
            column,
            {
                enableScripts: false,
                enableFindWidget: true,
                retainContextWhenHidden: true
            }
        );

        FunctionsReferencePanel.currentPanel = new FunctionsReferencePanel(panel, extensionUri);
    }

    private constructor(panel: vscode.WebviewPanel, extensionUri: vscode.Uri) {
        this.panel = panel;
        this.panel.webview.html = this.getHtml(panel.webview, extensionUri);
        this.panel.onDidDispose(() => {
            FunctionsReferencePanel.currentPanel = undefined;
        });
    }

    private getHtml(webview: vscode.Webview, extensionUri: vscode.Uri): string {
        const nonce = getNonce();

        return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src ${webview.cspSource} 'unsafe-inline';">
    <title>Silky Functions Reference</title>
    <style>
        body {
            font-family: var(--vscode-font-family);
            font-size: var(--vscode-font-size);
            color: var(--vscode-foreground);
            padding: 16px 24px;
            line-height: 1.5;
        }
        h1 { font-size: 1.6em; margin-bottom: 4px; }
        h2 {
            font-size: 1.15em;
            margin-top: 24px;
            margin-bottom: 8px;
            padding-bottom: 4px;
            border-bottom: 1px solid var(--vscode-panel-border);
            color: var(--vscode-textLink-foreground);
        }
        h3 {
            font-size: 1em;
            margin-top: 16px;
            margin-bottom: 6px;
            color: var(--vscode-textLink-activeForeground);
        }
        .subtitle {
            color: var(--vscode-descriptionForeground);
            margin-bottom: 16px;
        }
        table {
            border-collapse: collapse;
            width: 100%;
            margin-bottom: 12px;
        }
        th, td {
            text-align: left;
            padding: 4px 10px;
            border-bottom: 1px solid var(--vscode-panel-border);
        }
        th {
            font-weight: 600;
            color: var(--vscode-textLink-foreground);
        }
        code {
            font-family: var(--vscode-editor-font-family);
            font-size: 0.92em;
            background: var(--vscode-textCodeBlock-background);
            padding: 1px 5px;
            border-radius: 3px;
        }
        .where-table td:first-child { white-space: nowrap; }
        a { color: var(--vscode-textLink-foreground); }
    </style>
</head>
<body>
    <h1>Silky Functions Reference</h1>
    <p class="subtitle">Available template functions and jq operators for YAML configurations</p>

    <h2>Where to Use</h2>
    <table class="where-table">
        <tr><th>Syntax</th><th>Fields</th></tr>
        <tr><td>Go Templates + Sprig&ensp;<code>{{ }}</code></td><td><code>url</code>, <code>headers</code>, <code>body</code> (first pass)</td></tr>
        <tr><td>jq Expressions</td><td><code>body</code> (second pass), <code>resultTransformer</code>, <code>mergeOn</code>, <code>mergeWithParentOn</code>, <code>mergeWithContext</code>, <code>path</code></td></tr>
        <tr><td>Environment Variables</td><td>Everywhere in YAML &mdash; <code>\${VAR}</code> or <code>\${VAR:-default}</code></td></tr>
    </table>

    <h2>Sprig Template Functions</h2>

    <h3>String</h3>
    <table>
        <tr><th>Function</th><th>Example</th></tr>
        <tr><td><code>upper</code> / <code>lower</code></td><td><code>"hello" | upper</code> &rarr; <code>"HELLO"</code></td></tr>
        <tr><td><code>trim</code></td><td><code>"  hi  " | trim</code> &rarr; <code>"hi"</code></td></tr>
        <tr><td><code>trimPrefix</code> / <code>trimSuffix</code></td><td><code>"hello" | trimPrefix "hel"</code> &rarr; <code>"lo"</code></td></tr>
        <tr><td><code>replace</code></td><td><code>"hi" | replace "i" "ello"</code> &rarr; <code>"hello"</code></td></tr>
        <tr><td><code>contains</code></td><td><code>contains "ell" "hello"</code> &rarr; <code>true</code></td></tr>
        <tr><td><code>hasPrefix</code> / <code>hasSuffix</code></td><td><code>hasPrefix "hel" "hello"</code> &rarr; <code>true</code></td></tr>
        <tr><td><code>repeat</code></td><td><code>"ab" | repeat 3</code> &rarr; <code>"ababab"</code></td></tr>
        <tr><td><code>nospace</code></td><td><code>"h e l lo" | nospace</code> &rarr; <code>"hello"</code></td></tr>
        <tr><td><code>substr</code></td><td><code>substr 0 3 "hello"</code> &rarr; <code>"hel"</code></td></tr>
        <tr><td><code>quote</code> / <code>squote</code></td><td><code>"hi" | quote</code> &rarr; <code>"\"hi\""</code></td></tr>
        <tr><td><code>snakecase</code> / <code>camelcase</code></td><td><code>"myVar" | snakecase</code> &rarr; <code>"my_var"</code></td></tr>
        <tr><td><code>kebabcase</code></td><td><code>"myVar" | kebabcase</code> &rarr; <code>"my-var"</code></td></tr>
    </table>

    <h3>Math</h3>
    <table>
        <tr><th>Function</th><th>Example</th></tr>
        <tr><td><code>add</code> / <code>sub</code> / <code>mul</code> / <code>div</code></td><td><code>add 3 2</code> &rarr; <code>5</code>, <code>mul .from 1000</code></td></tr>
        <tr><td><code>mod</code></td><td><code>mod 10 3</code> &rarr; <code>1</code></td></tr>
        <tr><td><code>max</code> / <code>min</code></td><td><code>max 5 3</code> &rarr; <code>5</code></td></tr>
        <tr><td><code>floor</code> / <code>ceil</code> / <code>round</code></td><td><code>3.7 | floor</code> &rarr; <code>3</code></td></tr>
    </table>

    <h3>Date / Time</h3>
    <table>
        <tr><th>Function</th><th>Example</th></tr>
        <tr><td><code>now</code></td><td>Current time</td></tr>
        <tr><td><code>date</code></td><td><code>now | date "2006-01-02"</code> &rarr; <code>"2026-04-01"</code></td></tr>
        <tr><td><code>dateModify</code></td><td><code>now | dateModify "8760h"</code> (add ~1 year)</td></tr>
        <tr><td><code>unixEpoch</code></td><td><code>now | unixEpoch</code> &rarr; Unix seconds</td></tr>
        <tr><td><code>dateInZone</code></td><td><code>dateInZone "2006-01-02" (now) "UTC"</code></td></tr>
        <tr><td><code>toDate</code></td><td><code>toDate "2006-01-02" "2026-01-15"</code></td></tr>
    </table>
    <p><strong>Go date format reference:</strong> <code>2006</code>=year, <code>01</code>=month, <code>02</code>=day, <code>15</code>=hour, <code>04</code>=min, <code>05</code>=sec</p>

    <h3>Logic & Default</h3>
    <table>
        <tr><th>Function</th><th>Example</th></tr>
        <tr><td><code>default</code></td><td><code>default "N/A" .value</code></td></tr>
        <tr><td><code>empty</code></td><td><code>empty ""</code> &rarr; <code>true</code></td></tr>
        <tr><td><code>ternary</code></td><td><code>ternary "yes" "no" true</code> &rarr; <code>"yes"</code></td></tr>
        <tr><td><code>coalesce</code></td><td><code>coalesce .a .b "fallback"</code></td></tr>
    </table>

    <h3>Type Conversion</h3>
    <table>
        <tr><th>Function</th><th>Example</th></tr>
        <tr><td><code>toString</code> / <code>toInt</code> / <code>toFloat64</code></td><td><code>42 | toString</code> &rarr; <code>"42"</code></td></tr>
        <tr><td><code>toJson</code> / <code>toPrettyJson</code></td><td><code>.obj | toJson</code> &rarr; JSON string</td></tr>
    </table>

    <h3>Encoding</h3>
    <table>
        <tr><th>Function</th><th>Example</th></tr>
        <tr><td><code>b64enc</code> / <code>b64dec</code></td><td><code>"hello" | b64enc</code> &rarr; <code>"aGVsbG8="</code></td></tr>
        <tr><td><code>printf</code></td><td><code>printf "/Date(%d+0000)/" .epoch</code></td></tr>
    </table>

    <h3>List</h3>
    <table>
        <tr><th>Function</th><th>Example</th></tr>
        <tr><td><code>list</code></td><td><code>list "a" "b" "c"</code></td></tr>
        <tr><td><code>first</code> / <code>last</code></td><td><code>first (list 1 2 3)</code> &rarr; <code>1</code></td></tr>
        <tr><td><code>join</code></td><td><code>list "a" "b" | join ","</code> &rarr; <code>"a,b"</code></td></tr>
        <tr><td><code>sortAlpha</code></td><td><code>list "b" "a" | sortAlpha</code></td></tr>
    </table>

    <h2>jq Expressions</h2>

    <h3>Path & Selection</h3>
    <table>
        <tr><th>Operator</th><th>Example</th></tr>
        <tr><td><code>.field</code></td><td>Access object field</td></tr>
        <tr><td><code>.field.nested</code></td><td>Nested field access</td></tr>
        <tr><td><code>.[n]</code> / <code>.[n:m]</code></td><td>Array index / slice</td></tr>
        <tr><td><code>.[]</code></td><td>Iterate array</td></tr>
        <tr><td><code>select(cond)</code></td><td><code>select(.age > 18)</code></td></tr>
    </table>

    <h3>Array / Object Operations</h3>
    <table>
        <tr><th>Function</th><th>Example</th></tr>
        <tr><td><code>map(expr)</code></td><td><code>map(.id)</code> &mdash; apply to each element</td></tr>
        <tr><td><code>length</code></td><td>Array/string/object length</td></tr>
        <tr><td><code>keys</code> / <code>values</code></td><td>Object keys or values</td></tr>
        <tr><td><code>has("key")</code></td><td>Check key existence</td></tr>
        <tr><td><code>unique_by(.f)</code></td><td>Deduplicate by field</td></tr>
        <tr><td><code>group_by(.f)</code> / <code>sort_by(.f)</code></td><td>Group or sort by field</td></tr>
        <tr><td><code>flatten</code></td><td>Flatten nested arrays</td></tr>
        <tr><td><code>to_entries</code> / <code>from_entries</code></td><td>Convert to/from <code>[{key,value}]</code></td></tr>
        <tr><td><code>add</code></td><td>Sum numbers or concatenate arrays</td></tr>
    </table>

    <h3>String Operations</h3>
    <table>
        <tr><th>Function</th><th>Example</th></tr>
        <tr><td><code>split("/") / join(",")</code></td><td>Split/join strings</td></tr>
        <tr><td><code>startswith</code> / <code>endswith</code></td><td>String prefix/suffix check</td></tr>
        <tr><td><code>ltrimstr</code> / <code>rtrimstr</code></td><td>Trim string prefix/suffix</td></tr>
        <tr><td><code>ascii_downcase</code></td><td>Lowercase</td></tr>
        <tr><td><code>test("regex")</code></td><td>Regex match</td></tr>
    </table>

    <h3>Special Variables & Operators</h3>
    <table>
        <tr><th>Variable</th><th>Description</th></tr>
        <tr><td><code>$res</code></td><td>API response (in merge rules)</td></tr>
        <tr><td><code>$ctx</code></td><td>Full context map (in merge rules, body, resultTransformer)</td></tr>
        <tr><td><code>//</code></td><td>Alternative operator: <code>.val // "default"</code></td></tr>
        <tr><td><code>|</code></td><td>Pipe: <code>.items | map(.id)</code></td></tr>
        <tr><td><code>+=</code></td><td>Append: <code>.events += $res</code></td></tr>
    </table>

    <h2>Context Access</h2>
    <table>
        <tr><th>Context</th><th>Go Templates</th><th>jq Expressions</th></tr>
        <tr><td>Runtime variables</td><td><code>{{ .varName }}</code></td><td><code>.varName</code> or <code>$ctx.varName</code></td></tr>
        <tr><td>forEach context</td><td><code>{{ .contextName.field }}</code></td><td><code>.contextName.field</code></td></tr>
        <tr><td>API response</td><td>&mdash;</td><td><code>$res</code> (in merge rules)</td></tr>
    </table>

    <h2>External Documentation</h2>
    <p>
        <a href="https://masterminds.github.io/sprig/">Sprig Function Reference</a> &mdash; Full list of 100+ functions<br>
        <a href="https://jqlang.github.io/jq/manual/">jq Manual</a> &mdash; Complete jq language reference
    </p>
</body>
</html>`;
    }
}
