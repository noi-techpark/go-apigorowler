# Profiler Improvement Plan

## Executive Summary

This document outlines a comprehensive plan to improve the ApiGorowler profiler to provide maximum visibility into crawler execution, context management, transformations, and merging operations. The goal is to make it crystal clear to developers what is happening internally during crawl execution.

**Current Date**: 2025-11-04
**Status**: Planning Phase
**Estimated Timeline**: 7-8 weeks

---

## Problem Statement

### Current Issues

1. **Vague Event Semantics**
   - Generic names like "Response Transformation", "Response Merge-On"
   - Unclear what operation actually happened
   - No indication of which context was modified

2. **Missing Context Visibility**
   - Can't see the full context map state
   - Don't know which contexts exist at any point
   - Can't see context hierarchy (parent-child relationships)
   - Can't access historical snapshots of all contexts

3. **No Visual Diffs**
   - Hard to understand what changed
   - Before/after states not clearly shown
   - Can't see incremental mutations

4. **Performance Problems**
   - Deep copy on every profiler event (~15+ per request)
   - Large JSON responses copied repeatedly
   - Synchronous channel sends can block execution

5. **Parallelism Not Visible**
   - No indication of concurrent execution
   - Can't correlate events across threads
   - Order of parallel events unclear

6. **Hidden Operations**
   - URL template → final URL transformation hidden
   - jq transformation inputs/outputs not shown
   - Merge target context not specified
   - Context selection logic invisible

---

## Core Concepts to Visualize

### 1. Context System (Most Critical)

The crawler uses a **context map** where:
- Each step executes in a specific context
- Steps can modify:
  - Current context (by pointer)
  - Parent context (via `mergeWithParentOn`)
  - Named ancestor context (via `mergeWithContext`)
  - Global root context
- Multiple contexts exist simultaneously:
  ```
  contextMap = {
    "root": { Data: [...], Parent: "", Depth: 0 },
    "facility": { Data: {...}, Parent: "root", Depth: 1 },
    "room": { Data: {...}, Parent: "facility", Depth: 2 }
  }
  ```

**Developer Needs:**
- See ALL contexts at any point in time
- Understand context hierarchy (root → facility → room)
- Access snapshots of all contexts, not just current
- See which context a merge operation targeted
- Understand template context (which contexts are accessible)

### 2. Request Lifecycle

```
Template Evaluation → URL Composition → HTTP Request →
Raw Response → Transformation → Merge to Target Context
```

**What to Show:**
- Template variables and their values from context
- Final composed URL with query params
- HTTP method, headers, status code, timing
- Raw JSON response body
- jq expression applied to response
- Transformation result
- Which context received the merge
- Merge rule and before/after diff

### 3. ForEach Lifecycle

```
Item Extraction → Context Creation → Nested Execution →
Result Collection → Merge Back
```

**What to Show:**
- Path/values expression used
- Array of items extracted
- How many contexts created
- Parallel vs sequential execution
- Each iteration's context state
- How results merged back to parent/target
- Which context in the map received results

### 4. Merge Operations

**Three merge strategies:**
- `mergeOn`: Merge to current/ancestor context
- `mergeWithParentOn`: Merge to immediate parent
- `mergeWithContext`: Merge to named context
- `noopMerge`: No merge (side-effects only)

**What to Show:**
- Source data (what's being merged)
- Target context key (which context in the map)
- Merge rule/expression
- Target context state before merge
- Target context state after merge
- Diff of changes to target

### 5. Parallel Execution

**What to Show:**
- Thread/goroutine ID for each operation
- Logical iteration number vs execution order
- Timeline of concurrent operations
- Rate limiting throttling
- Which operations happened simultaneously

---

## Proposed Architecture

### Two-Tier Profiling System

#### Tier 1: Lightweight Events (During Execution)

**Emit minimal events** with references:

```go
type ProfileEvent struct {
    ID           string           `json:"id"`
    ParentID     string           `json:"parentId"`
    Type         ProfileEventType `json:"type"`
    Timestamp    time.Time        `json:"timestamp"`
    ThreadID     int              `json:"threadId"`
    ContextKey   string           `json:"contextKey"` // Which context executing in
    IterationNum int              `json:"iterationNum"`
    Duration     time.Duration    `json:"duration,omitempty"`

    // Lightweight metadata
    Metadata EventMetadata `json:"metadata"`

    // Snapshot references (not full data)
    SnapshotRefs SnapshotReferences `json:"snapshotRefs"`
}
```

**Advantages:**
- Low memory footprint during execution
- No expensive deep copies in hot path
- Events buffered and ordered
- Parallel-safe

#### Tier 2: Snapshot Store (On Demand)

**Store full context map snapshots** at mutation points:

```go
type ContextMapSnapshot struct {
    ID        string                 `json:"id"`
    Timestamp time.Time              `json:"timestamp"`
    EventID   string                 `json:"eventId"` // Which event created this
    Contexts  map[string]ContextData `json:"contexts"` // Full context map
}

type ContextData struct {
    Data          any    `json:"data"`
    ParentContext string `json:"parentContext"`
    Depth         int    `json:"depth"`
    Key           string `json:"key"`
}
```

**When to snapshot:**
- Before/after each merge operation
- Before/after transformations
- After each forEach iteration
- On explicit context mutations

**Advantages:**
- Developer sees ALL contexts, not just current
- Can inspect any context at any snapshot point
- Compute diffs lazily when UI requests
- Extension fetches only what's viewed

---

## Enhanced Event Model

### Event Types (Semantic, Not Generic)

```go
type ProfileEventType int

const (
    // Request lifecycle
    EVENT_URL_TEMPLATE_EVAL  ProfileEventType = iota // Template + vars → URL
    EVENT_URL_COMPOSED                               // Final URL ready
    EVENT_HTTP_REQUEST_START                         // HTTP call initiated
    EVENT_HTTP_RESPONSE                              // Response received
    EVENT_RESPONSE_PARSED                            // JSON parsed
    EVENT_TRANSFORM_APPLY                            // jq transformation
    EVENT_MERGE_EXECUTE                              // Merge to target context

    // ForEach lifecycle
    EVENT_FOREACH_EXTRACT    // Path evaluated → items
    EVENT_FOREACH_START      // Begin iterations
    EVENT_ITERATION_START    // Single iteration starts
    EVENT_CONTEXT_CREATED    // New child context in map
    EVENT_NESTED_EXEC_START  // Nested steps executing
    EVENT_NESTED_EXEC_END    // Nested steps complete
    EVENT_ITERATION_END      // Iteration result ready
    EVENT_FOREACH_MERGE      // Results merged back
    EVENT_FOREACH_END        // All iterations complete

    // Context operations
    EVENT_CONTEXT_MAP_SNAPSHOT // Full context map state
    EVENT_CONTEXT_MUTATED      // Specific context modified
    EVENT_CONTEXT_SWITCHED     // Execution moved to different context

    // Parallelism
    EVENT_PARALLEL_START     // Parallel execution begins
    EVENT_WORKER_STARTED     // Worker goroutine started
    EVENT_RATE_LIMITED       // Request throttled
    EVENT_PARALLEL_COMPLETE  // All parallel work done
)
```

### Event Data Structure

```go
type EventMetadata struct {
    // URL Composition
    URLTemplate     string            `json:"urlTemplate,omitempty"`
    TemplateVars    map[string]any    `json:"templateVars,omitempty"` // Variables from context
    ComposedURL     string            `json:"composedUrl,omitempty"`

    // HTTP Request/Response
    Method          string            `json:"method,omitempty"`
    StatusCode      int               `json:"statusCode,omitempty"`
    Headers         map[string]string `json:"headers,omitempty"`
    QueryParams     map[string]string `json:"queryParams,omitempty"`
    BodyParams      map[string]string `json:"bodyParams,omitempty"`
    ResponseSize    int               `json:"responseSize,omitempty"`

    // Transformation
    Expression      string `json:"expression,omitempty"`      // jq expression
    ExpressionType  string `json:"expressionType,omitempty"` // "resultTransformer", "mergeOn", etc

    // Merge Details
    TargetContextKey string `json:"targetContextKey,omitempty"` // Which context in map
    MergeStrategy    string `json:"mergeStrategy,omitempty"`    // mergeOn/mergeWithParent/etc
    MergeRule        string `json:"mergeRule,omitempty"`        // The actual expression

    // ForEach Details
    Path            string `json:"path,omitempty"`
    ItemCount       int    `json:"itemCount,omitempty"`
    ItemValue       any    `json:"itemValue,omitempty"`
    AsKey           string `json:"asKey,omitempty"`    // Context key for this item
    Parallel        bool   `json:"parallel,omitempty"`
    MaxConcurrency  int    `json:"maxConcurrency,omitempty"`

    // Context Hierarchy
    ContextHierarchy []string `json:"contextHierarchy,omitempty"` // ["root", "facility", "room"]
    AvailableContexts []string `json:"availableContexts,omitempty"` // All keys in context map
}

type SnapshotReferences struct {
    // Before/after for data transformations
    InputSnapshotID  string `json:"inputSnapshotId,omitempty"`
    OutputSnapshotID string `json:"outputSnapshotId,omitempty"`

    // Before/after for context mutations
    ContextMapBeforeID string `json:"contextMapBeforeId,omitempty"`
    ContextMapAfterID  string `json:"contextMapAfterId,omitempty"`

    // Specific context snapshots
    TargetContextBeforeID string `json:"targetContextBeforeId,omitempty"`
    TargetContextAfterID  string `json:"targetContextAfterId,omitempty"`
}
```

---

## Visualization Strategy

### 1. Enhanced Tree View (Primary Navigation)

**Hierarchical structure with semantic icons:**

```
📋 Crawl Execution
├─ 🔄 Request 'get-facilities' | page#1 | ⏱ 234ms
│   ├─ 🔗 URL Template Evaluated
│   │   └─ Template: https://api.example.com/facilities?offset={{.pagination.offset}}
│   │   └─ Variables: { pagination: { offset: 0 } }
│   │   └─ Result: https://api.example.com/facilities?offset=0
│   ├─ 📡 HTTP GET 200 OK | 234ms
│   │   └─ Response: 15.2 KB
│   ├─ 📥 Response Parsed
│   │   └─ 50 facilities extracted
│   ├─ ⚙️  Transform: .data
│   │   └─ Input: object → Output: array[50]
│   ├─ 🔀 Merge to 'root'
│   │   └─ Rule: . = $res
│   │   └─ Context: root | Before: [] → After: array[50]
│   └─ 📂 ForEach 'process-facility' | 50 items | ⚡ parallel (10 workers)
│       ├─ 🔍 Extract Items: .
│       │   └─ 50 items from root context
│       ├─ ⚡ Iteration #0 | Thread 5 | Context 'facility'
│       │   ├─ 🆕 Context Created: 'facility'
│       │   │   └─ Parent: root | Data: { id: "F1", name: "Building A" }
│       │   ├─ 🔄 Request 'get-rooms'
│       │   │   ├─ 🔗 URL: https://api.example.com/facilities/F1/rooms
│       │   │   ├─ 📡 HTTP GET 200 OK | 156ms
│       │   │   ├─ ⚙️  Transform: .data
│       │   │   └─ 🔀 Merge to 'facility'
│       │   │       └─ Rule: .rooms = $res
│       │   │       └─ Context: facility | Before: { id: "F1" } → After: { id: "F1", rooms: [...] }
│       │   └─ ✅ Iteration Complete | Result merged to root[0]
│       ├─ ⚡ Iteration #1 | Thread 6 | Context 'facility'
│       │   └─ ...
│       └─ ✅ ForEach Complete | 50 iterations done
└─ ✅ Crawl Complete

Icons:
🔄 Request start        📡 HTTP call         📥 Response
🔗 URL composition      ⚙️  Transformation    🔀 Merge
📂 ForEach start        🔍 Item extraction   ⚡ Parallel iteration
🔁 Sequential iter      🆕 Context created   ✅ Complete
⏱ Duration indicator    ⚠️  Warning/error
```

**Interactive features:**
- Click any event → Shows detail panel
- Hover → Shows quick tooltip
- Expand/collapse nested operations
- Filter by thread ID
- Color-code by context key

### 2. Enhanced Detail Panel

#### **Tab 1: Overview**

```
┌─ Event Overview ───────────────────────────────────┐
│ Event Type: HTTP GET Request                       │
│ Event ID: evt-001-req-123                          │
│ Timestamp: 2025-11-04 10:45:23.456                 │
│ Duration: 234ms                                    │
│ Thread: 5 (parallel)                               │
│ Executing In: root context                         │
│                                                     │
│ Context Hierarchy:                                 │
│   root (current) ← You are here                    │
│                                                     │
│ Available Contexts:                                │
│   • root (array[50])                               │
│   • facility (object) - 10 instances               │
│   • room (object) - 150 instances                  │
└─────────────────────────────────────────────────────┘
```

#### **Tab 2: Request Details** (for HTTP events)

```
┌─ HTTP Request ─────────────────────────────────────┐
│ Method: GET                                        │
│ URL: https://api.example.com/facilities?offset=0  │
│                                                     │
│ URL Composition:                                   │
│   Template: /facilities?offset={{.pagination.offset}}│
│   Variables from context:                          │
│     • pagination.offset = 0                        │
│                                                     │
│ Query Parameters:                                  │
│   • offset: 0                                      │
│   • limit: 50                                      │
│                                                     │
│ Headers:                                           │
│   • Authorization: Bearer ***                      │
│   • Content-Type: application/json                 │
│                                                     │
│ Response:                                          │
│   Status: 200 OK                                   │
│   Size: 15.2 KB                                    │
│   Duration: 234ms                                  │
└─────────────────────────────────────────────────────┘
```

#### **Tab 3: Data & Transformation**

```
┌─ Transformation ───────────────────────────────────┐
│ Expression: .data | map(select(.active == true))   │
│ Type: resultTransformer                            │
│                                                     │
│ Input (from snapshot #123):                        │
│ {                                                   │
│   "data": [                                        │
│     { "id": "F1", "active": true, ... },          │
│     { "id": "F2", "active": false, ... },         │
│     { "id": "F3", "active": true, ... }           │
│   ],                                               │
│   "total": 50                                      │
│ }                                                   │
│                                                     │
│ Output (snapshot #124):                            │
│ [                                                   │
│   { "id": "F1", "active": true, ... },            │
│   { "id": "F3", "active": true, ... }             │
│ ]                                                   │
│                                                     │
│ Summary:                                           │
│   • Filtered 50 → 2 items                          │
│   • Selected active facilities only                │
└─────────────────────────────────────────────────────┘

Tabs: [ Input ] [ Output ] [ Diff ] [ Summary ]
```

**Diff View:**

```
┌─ Data Diff ────────────────────────────────────────┐
│ Changes to data:                                   │
│                                                     │
│ - Removed: { "data": { ... }, "total": 50 }       │
│ + Added: [ ... ]                                   │
│                                                     │
│ Structural change: object → array                  │
│                                                     │
│ Content:                                           │
│   • Kept 2 of 50 items                             │
│   • Removed items where active == false            │
└─────────────────────────────────────────────────────┘
```

#### **Tab 4: Context Inspector** (CRITICAL)

**Shows ALL contexts in the context map at this snapshot:**

```
┌─ Context Map (Snapshot #125) ──────────────────────┐
│ Snapshot Time: 2025-11-04 10:45:23.690            │
│ After Event: Merge to 'facility' context          │
│                                                     │
│ All Contexts:                                      │
│                                                     │
│ ┌─ root (array[50]) ──────────┐                   │
│ │ Depth: 0                     │                   │
│ │ Parent: none                 │                   │
│ │ [                            │                   │
│ │   { "id": "F1", "name": "Building A", ...}, │   │
│ │   { "id": "F2", "name": "Building B", ...}, │   │
│ │   ...                        │                   │
│ │ ]                            │                   │
│ └──────────────────────────────┘                   │
│                                                     │
│ ┌─ facility (object) ●─────────┐  ← Modified      │
│ │ Depth: 1                     │                   │
│ │ Parent: root                 │                   │
│ │ {                            │                   │
│ │   "id": "F1",               │                   │
│ │   "name": "Building A",     │                   │
│ │   "rooms": [...]  ← NEW     │                   │
│ │ }                            │                   │
│ └──────────────────────────────┘                   │
│                                                     │
│ ┌─ room (object) ──────────────┐                   │
│ │ Depth: 2                     │                   │
│ │ Parent: facility             │                   │
│ │ {                            │                   │
│ │   "id": "R1",               │                   │
│ │   "capacity": 50            │                   │
│ │ }                            │                   │
│ └──────────────────────────────┘                   │
│                                                     │
│ Context Hierarchy:                                 │
│   root → facility → room                           │
│                                                     │
│ Template Access:                                   │
│   All contexts accessible via:                     │
│     • .root (root context data)                    │
│     • .facility (facility context data)            │
│     • .room (room context data)                    │
└─────────────────────────────────────────────────────┘

Controls:
[ < Previous Snapshot ] [ Next Snapshot > ]
[ View Context Timeline ]
[ Export All Contexts ]
```

**Context Timeline (Sub-view):**

```
┌─ Context Timeline: 'facility' ─────────────────────┐
│                                                     │
│ Snapshot #120 (Before iteration start)             │
│   { }  (empty, just created)                       │
│                                                     │
│ Snapshot #121 (After item binding)                 │
│   {                                                 │
│     "id": "F1",                                    │
│     "name": "Building A"                           │
│   }                                                 │
│   ↑ Item bound from parent                         │
│                                                     │
│ Snapshot #123 (After get-rooms request)            │
│   {                                                 │
│     "id": "F1",                                    │
│     "name": "Building A",                          │
│     "rooms": [                                     │
│       { "id": "R1", ... },                         │
│       { "id": "R2", ... }                          │
│     ]                                               │
│   }                                                 │
│   ↑ Merged rooms array                             │
│                                                     │
│ Snapshot #125 (Merged back to parent)              │
│   (Same as #123, no local changes)                 │
│   ↑ Result copied to root[0]                       │
│                                                     │
└─────────────────────────────────────────────────────┘
```

#### **Tab 5: Merge Operation** (for merge events)

```
┌─ Merge Operation ──────────────────────────────────┐
│ Operation: Merge result to context                 │
│ Strategy: mergeWithContext                         │
│ Rule: .rooms = $res                                │
│                                                     │
│ Source:                                            │
│   • Data: array[15] (rooms from API response)     │
│   • Snapshot: #122                                 │
│                                                     │
│ Target Context: 'facility'                         │
│   • Key in context map: "facility"                 │
│   • Parent: root                                   │
│   • Depth: 1                                       │
│                                                     │
│ Before Merge (Snapshot #121):                      │
│ {                                                   │
│   "id": "F1",                                      │
│   "name": "Building A"                             │
│ }                                                   │
│                                                     │
│ After Merge (Snapshot #123):                       │
│ {                                                   │
│   "id": "F1",                                      │
│   "name": "Building A",                            │
│   "rooms": [...]  ← ADDED                          │
│ }                                                   │
│                                                     │
│ Diff:                                              │
│   + Added field: rooms (array[15])                 │
│   • No fields removed                              │
│   • No fields modified                             │
│                                                     │
│ Other Contexts (Unchanged):                        │
│   • root: No changes                               │
│   • room: Not affected                             │
└─────────────────────────────────────────────────────┘

Tabs: [ Before ] [ After ] [ Diff ] [ All Contexts ]
```

### 3. New: Timeline View (Parallelism Visualization)

```
┌─ Execution Timeline ───────────────────────────────────────────────────────┐
│                                                                             │
│ Thread 1  |███████[Get Facilities]███████|      |████[Merge]████|          │
│ Thread 2  |    |███[Iter #0]███|    |███[Iter #5]███|                      │
│ Thread 3  |    |███[Iter #1]███|    |███[Iter #6]███|                      │
│ Thread 4  |         |███[Iter #2]███|    |███[Iter #7]███|                 │
│ Thread 5  |         |███[Iter #3]███|         |███[Iter #8]███|            │
│ Thread 6  |              |███[Iter #4]███|         |███[Iter #9]███|       │
│           ├────────┬────────┬────────┬────────┬────────┬────────┬───────>  │
│           0ms    100ms    200ms    300ms    400ms    500ms    600ms  Time  │
│                                                                             │
│ Legend:                                                                     │
│   ███ Request   ███ ForEach   ███ Transform   ███ Merge                    │
│                                                                             │
│ Stats:                                                                      │
│   Total Duration: 623ms                                                    │
│   Parallel Threads: 6                                                      │
│   Concurrent Ops: Max 5 at 250ms                                           │
│   Rate Limited: 3 times                                                    │
└─────────────────────────────────────────────────────────────────────────────┘

Interactions:
• Click block → Jump to event in tree
• Hover → Show event tooltip
• Drag to zoom time range
• Click thread → Filter tree to thread
```

### 4. New: Context Map Explorer

**Dedicated view for exploring all contexts:**

```
┌─ Context Map Explorer ─────────────────────────────────────────────────────┐
│                                                                             │
│ Select Snapshot: [Dropdown: #125 - After Iteration #0 complete]            │
│                                                                             │
│ ┌─ Context Hierarchy ────────┐  ┌─ Selected Context ──────────────────────┐│
│ │                            │  │ Context: facility                        ││
│ │ • root (array[50])         │  │ Key: "facility"                          ││
│ │   └─ facility (object) ●   │  │ Parent: root                             ││
│ │       └─ room (object)     │  │ Depth: 1                                 ││
│ │                            │  │ Created: Iteration #0                    ││
│ │ Click to view context →    │  │                                          ││
│ └────────────────────────────┘  │ Data:                                    ││
│                                  │ {                                        ││
│ ┌─ Context Timeline ─────────┐  │   "id": "F1",                            ││
│ │ facility:                  │  │   "name": "Building A",                  ││
│ │                            │  │   "active": true,                        ││
│ │ #120: { } (created)        │  │   "rooms": [                             ││
│ │ #121: {...} (item bound)   │  │     { "id": "R1", "capacity": 50 },     ││
│ │ #123: {...} (rooms added) ●│  │     { "id": "R2", "capacity": 30 }      ││
│ │ #125: {...} (merged back)  │  │   ]                                      ││
│ │                            │  │ }                                        ││
│ │ Click to jump to snapshot  │  │                                          ││
│ └────────────────────────────┘  │ Mutations:                               ││
│                                  │   • Created: Event #10 (Iteration start)││
│                                  │   • Modified: Event #15 (Merge rooms)   ││
│                                  │   • Merged: Event #20 (Iteration end)   ││
│                                  └──────────────────────────────────────────┘│
│                                                                             │
│ ┌─ All Contexts at Snapshot #125 ───────────────────────────────────────┐  │
│ │                                                                        │  │
│ │ root (array[50]):                                                     │  │
│ │   [                                                                   │  │
│ │     { "id": "F1", "name": "Building A", "rooms": [...] },  ← Updated │  │
│ │     { "id": "F2", "name": "Building B" },                  ← Pending │  │
│ │     ...                                                               │  │
│ │   ]                                                                   │  │
│ │                                                                        │  │
│ │ facility (object):                                                    │  │
│ │   { "id": "F1", "name": "Building A", "rooms": [...] }               │  │
│ │                                                                        │  │
│ │ room (object):                                                        │  │
│ │   { "id": "R1", "capacity": 50 }                                     │  │
│ │                                                                        │  │
│ └────────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
│ Controls:                                                                   │
│ [ Export All Contexts as JSON ] [ Compare Snapshots ] [ View Diff ]        │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Features:**
- View ALL contexts at any snapshot
- Navigate context timeline
- Compare snapshots side-by-side
- See which events modified which contexts
- Export full context map state
- Understand template variable access

---

## Implementation Phases

### Phase 1: Enhanced Event Types (Week 1)

**Goal**: Replace generic events with semantic, operation-specific events

**Tasks:**
- [ ] Define comprehensive `ProfileEventType` enum in `crawler.go`
- [ ] Create `EventMetadata` and `SnapshotReferences` structs
- [ ] Update all `pushProfilerData()` calls to use specific event types:
  - [ ] `EVENT_URL_TEMPLATE_EVAL` with template + variables
  - [ ] `EVENT_HTTP_REQUEST_START` with method, headers, params
  - [ ] `EVENT_HTTP_RESPONSE` with status, size, duration
  - [ ] `EVENT_TRANSFORM_APPLY` with expression, type
  - [ ] `EVENT_MERGE_EXECUTE` with target context key, strategy, rule
  - [ ] `EVENT_FOREACH_EXTRACT` with path, item count
  - [ ] `EVENT_ITERATION_START` with iteration number, item value
  - [ ] `EVENT_CONTEXT_CREATED` with context key, parent, depth
- [ ] Update TypeScript interfaces in `stepsTreeProvider.ts`
- [ ] Display semantic event names in tree view with icons
- [ ] Add event type filter to tree view

**Deliverables:**
- Events have clear, descriptive names
- Tree view shows operation hierarchy clearly
- Developer can understand flow at a glance

**Testing:**
- Run simple request config
- Verify event names make sense
- Check tree hierarchy is logical

### Phase 2: Context Snapshot System (Week 2)

**Goal**: Implement snapshot storage for full context map at mutation points

**Tasks:**
- [ ] Create `ContextMapSnapshot` struct in `crawler.go`
- [ ] Implement snapshot storage:
  - [ ] In-memory map: `snapshotID → ContextMapSnapshot`
  - [ ] Generate unique snapshot IDs
  - [ ] Store full context map (all contexts, not just current)
- [ ] Identify snapshot points:
  - [ ] Before/after each merge operation
  - [ ] Before/after transformations
  - [ ] After context creation
  - [ ] After each forEach iteration
- [ ] Add snapshot references to `ProfileEvent`
- [ ] Implement snapshot retrieval API in CLI
- [ ] Update extension to request snapshots on demand
- [ ] Show "Loading snapshot..." placeholder in UI

**Deliverables:**
- Context map snapshots captured at key points
- Extension can fetch snapshots by ID
- All contexts visible at each snapshot

**Testing:**
- Run forEach with nested contexts
- Verify snapshots contain all contexts
- Check parent-child relationships preserved

### Phase 3: Context Inspector UI (Week 3)

**Goal**: Build UI to visualize ALL contexts at any snapshot

**Tasks:**
- [ ] Create "Context Inspector" tab in detail panel
- [ ] Display context hierarchy tree (root → parent → child)
- [ ] Show all contexts in snapshot:
  - [ ] Context key
  - [ ] Parent context
  - [ ] Depth level
  - [ ] Full data (expandable JSON)
- [ ] Implement context timeline:
  - [ ] List all snapshots for a specific context
  - [ ] Show mutations over time
  - [ ] Navigate between snapshots
- [ ] Add template variable viewer:
  - [ ] Show which contexts are accessible
  - [ ] Display available template variables
- [ ] Implement snapshot comparison:
  - [ ] Select two snapshots
  - [ ] Show side-by-side diff
- [ ] Add "Export Context Map" button

**Deliverables:**
- Developer can see ALL contexts at any point
- Context hierarchy is clear
- Can navigate context mutations over time

**Testing:**
- Run multi-level forEach (root → facility → room)
- Verify all 3 contexts visible
- Check context timeline shows mutations

### Phase 4: Request Lifecycle Events (Week 4)

**Goal**: Detailed visibility into URL composition, HTTP, and transformations

**Tasks:**
- [ ] Emit `EVENT_URL_TEMPLATE_EVAL`:
  - [ ] Capture template string
  - [ ] Capture template variables from context
  - [ ] Capture composed URL
- [ ] Enhance HTTP events:
  - [ ] Separate request start and response events
  - [ ] Include method, headers, query params, body params
  - [ ] Capture status code, response size, duration
- [ ] Improve transformation events:
  - [ ] Capture input data snapshot
  - [ ] Capture output data snapshot
  - [ ] Include expression and type (resultTransformer, mergeOn, etc)
- [ ] Update detail panel "Request" tab:
  - [ ] Show URL composition section
  - [ ] Display template variables
  - [ ] Show final URL with all params
- [ ] Update "Data & Transformation" tab:
  - [ ] Show input data
  - [ ] Show output data
  - [ ] Display transformation expression
  - [ ] Add summary (e.g., "Filtered 50 → 2 items")

**Deliverables:**
- Complete visibility into request lifecycle
- Can see how URLs are composed from templates
- Transformation inputs/outputs clearly shown

**Testing:**
- Use config with URL template: `/items/{{.item.id}}`
- Verify template variables shown
- Check transformation displays input → output

### Phase 5: Merge Operation Visibility (Week 5)

**Goal**: Make merge operations crystal clear

**Tasks:**
- [ ] Emit `EVENT_MERGE_EXECUTE` with:
  - [ ] Target context key (which context in map)
  - [ ] Merge strategy (mergeOn/mergeWithParent/mergeWithContext)
  - [ ] Merge rule expression
  - [ ] Source data snapshot ID
  - [ ] Target context before/after snapshot IDs
- [ ] Create "Merge Operation" tab in detail panel:
  - [ ] Show source data
  - [ ] Show target context key
  - [ ] Display merge strategy and rule
  - [ ] Show before/after state of TARGET context
  - [ ] List other contexts (unchanged)
- [ ] Implement context-specific diff:
  - [ ] Highlight changes to target context only
  - [ ] Show added/removed/modified fields
  - [ ] Indicate structural changes (object → array)
- [ ] Add merge visualization to tree:
  - [ ] Icon shows merge direction: ↓ (to current), ↑ (to parent), ↗ (to named)
  - [ ] Tooltip shows target context key

**Deliverables:**
- Merge target context always visible
- Before/after diffs show exact changes
- Developer understands where data went

**Testing:**
- Use config with `mergeWithParentOn`
- Verify parent context shown as target
- Check diff shows parent mutations only
- Test with `mergeWithContext` to named context

### Phase 6: Diff Visualization (Week 6)

**Goal**: Rich diffs with syntax highlighting and summaries

**Tasks:**
- [ ] Install diff library in extension (`diff` or `jsondiffpatch`)
- [ ] Implement diff computation:
  - [ ] Fetch before/after snapshots
  - [ ] Compute JSON diff
  - [ ] Cache computed diffs
- [ ] Create diff viewer component:
  - [ ] Side-by-side view (before | after)
  - [ ] Unified view with +/- indicators
  - [ ] Syntax highlighting for JSON
  - [ ] Highlight changed lines
- [ ] Add diff summary:
  - [ ] Count added/removed/modified fields
  - [ ] Show structural changes (type changes)
  - [ ] Display array changes (items added/removed)
- [ ] Implement diff modes:
  - [ ] Data diff (for transformations)
  - [ ] Context diff (for merges)
  - [ ] Full context map diff (all contexts)

**Deliverables:**
- Beautiful, readable diffs
- Summary shows what changed at a glance
- Multiple diff views for different needs

**Testing:**
- Transform that filters array
- Merge that adds fields to object
- Verify diffs are accurate and clear

### Phase 7: Parallelism Support (Week 7)

**Goal**: Visualize concurrent execution

**Tasks:**
- [ ] Add thread ID to events:
  - [ ] Capture goroutine ID (use `runtime.GoID()` or similar)
  - [ ] Include in `ProfileEvent`
- [ ] Add execution ID for correlation:
  - [ ] Generate ID for each parallel batch
  - [ ] Link related events across threads
- [ ] Implement event buffering:
  - [ ] Buffer events during parallel execution
  - [ ] Sort by timestamp + iteration number
  - [ ] Preserve logical order despite concurrency
- [ ] Update tree view:
  - [ ] Show thread ID on parallel iterations
  - [ ] Icon indicates parallel: ⚡ vs sequential: 🔁
  - [ ] Color-code by thread
- [ ] Add parallelism metadata:
  - [ ] Max concurrency
  - [ ] Rate limiting events
  - [ ] Thread utilization

**Deliverables:**
- Parallel iterations clearly marked
- Thread IDs shown
- Events properly ordered despite concurrency

**Testing:**
- Run forEach with `parallel: true`
- Verify thread IDs shown
- Check iterations ordered correctly

### Phase 8: Timeline View (Week 8)

**Goal**: Visual timeline of execution across threads

**Tasks:**
- [ ] Create timeline component:
  - [ ] Horizontal time axis (0ms → total duration)
  - [ ] Vertical lanes for threads
  - [ ] Blocks for operations
- [ ] Calculate block positions:
  - [ ] Start time from event timestamp
  - [ ] Width from duration
  - [ ] Lane from thread ID
- [ ] Color-code operations:
  - [ ] Requests: blue
  - [ ] Transformations: green
  - [ ] Merges: orange
  - [ ] ForEach: purple
- [ ] Add interactions:
  - [ ] Click block → Jump to event in tree
  - [ ] Hover → Tooltip with event details
  - [ ] Zoom time range
  - [ ] Filter by thread
- [ ] Show rate limiting:
  - [ ] Gaps where throttled
  - [ ] Tooltip explains delay

**Deliverables:**
- Visual timeline of execution
- Easy to see parallel operations
- Understand performance bottlenecks

**Testing:**
- Run parallel forEach
- Verify timeline shows concurrent blocks
- Check zoom and interaction work

### Phase 9: Performance Optimization (Week 9)

**Goal**: Reduce profiler overhead, improve responsiveness

**Tasks:**
- [ ] Optimize snapshot storage:
  - [ ] Only snapshot on mutations (not every event)
  - [ ] Snapshot only modified contexts (not entire map)
  - [ ] Use copy-on-write for unchanged contexts
- [ ] Lazy data loading:
  - [ ] Extension requests snapshots on expand
  - [ ] Cache snapshots in extension
  - [ ] Paginate large arrays
- [ ] Diff computation:
  - [ ] Compute diffs in background thread
  - [ ] Cache computed diffs
  - [ ] Cancel diff if user navigates away
- [ ] Event streaming:
  - [ ] Use buffered channel for profiler events
  - [ ] Batch events before sending to extension
  - [ ] Limit event rate (e.g., max 100/sec)
- [ ] Memory management:
  - [ ] Limit snapshot storage (e.g., max 1000 snapshots)
  - [ ] Evict old snapshots when limit reached
  - [ ] Allow developer to clear snapshots

**Deliverables:**
- Profiler overhead reduced by ~70-80%
- Extension responsive with large datasets
- Memory usage controlled

**Testing:**
- Run large crawl (1000+ requests)
- Measure profiler overhead (time, memory)
- Verify extension remains responsive

### Phase 10: Context Map Explorer (Week 10)

**Goal**: Dedicated view for deep context exploration

**Tasks:**
- [ ] Create new view panel "Context Map Explorer"
- [ ] Implement context hierarchy tree:
  - [ ] Expandable tree of contexts
  - [ ] Show context key, parent, depth
  - [ ] Click to select context
- [ ] Display selected context:
  - [ ] Full data (formatted JSON)
  - [ ] Metadata (parent, depth, created by)
  - [ ] List of mutations
- [ ] Add context timeline:
  - [ ] All snapshots for selected context
  - [ ] Navigate between snapshots
  - [ ] Show which events modified it
- [ ] Implement snapshot comparison:
  - [ ] Select two snapshots
  - [ ] Side-by-side diff
  - [ ] Highlight changes
- [ ] Add export features:
  - [ ] Export single context as JSON
  - [ ] Export all contexts as JSON
  - [ ] Export snapshot history

**Deliverables:**
- Dedicated space for context exploration
- Easy navigation between contexts
- Deep visibility into context evolution

**Testing:**
- Run nested forEach (3 levels deep)
- Verify hierarchy shows all levels
- Check timeline shows mutations correctly

---

## Performance Targets

### Current Performance Issues

| Metric | Current | Problem |
|--------|---------|---------|
| Deep copies per request | 15-20 | Too many |
| Data copied per request | 15-20x response size | Huge overhead |
| Profiler overhead | 30-40% | Unacceptable |
| Memory usage | 5-10x raw data | Too high |

### Target Performance

| Metric | Target | How |
|--------|--------|-----|
| Snapshots per request | 3-5 | Only on mutations |
| Data copied per request | 3-5x response size | Snapshot refs |
| Profiler overhead | 5-10% | Lazy loading |
| Memory usage | 2-3x raw data | Smart snapshots |

---

## API Changes Required

### Go (crawler.go)

**New exports:**
```go
// Snapshot retrieval
func (a *ApiCrawler) GetSnapshot(snapshotID string) (*ContextMapSnapshot, error)
func (a *ApiCrawler) GetAllSnapshots() map[string]*ContextMapSnapshot

// Context inspection
func (a *ApiCrawler) GetContextMap() map[string]*Context
func (a *ApiCrawler) GetContextHierarchy() []string
```

### CLI (cmd/cli/main.go)

**New flags:**
```go
-profiler-snapshots   Enable snapshot storage
-profiler-max-snapshots int  Max snapshots to keep (default 1000)
-profiler-stream-snapshots  Stream snapshots to stdout
```

**New output format:**
```json
{
  "type": "event",
  "data": { ... ProfileEvent ... }
}

{
  "type": "snapshot",
  "id": "snap-123",
  "data": { ... ContextMapSnapshot ... }
}
```

### Extension

**New requests to CLI:**
```typescript
interface SnapshotRequest {
  type: 'getSnapshot';
  snapshotId: string;
}

interface SnapshotResponse {
  snapshotId: string;
  timestamp: string;
  contexts: Record<string, ContextData>;
}
```

---

## Testing Strategy

### Unit Tests

- [ ] Test event generation for each operation type
- [ ] Test snapshot creation at mutation points
- [ ] Test snapshot ID generation (uniqueness)
- [ ] Test context hierarchy computation
- [ ] Test diff computation (add/remove/modify)

### Integration Tests

- [ ] Test complete request lifecycle profiling
- [ ] Test forEach with nested contexts
- [ ] Test parallel forEach event ordering
- [ ] Test merge to different context keys
- [ ] Test snapshot retrieval by ID

### UI Tests

- [ ] Test tree view with complex hierarchy
- [ ] Test detail panel tabs
- [ ] Test context inspector navigation
- [ ] Test timeline view interactions
- [ ] Test diff viewer accuracy

### Performance Tests

- [ ] Measure profiler overhead with profiling on/off
- [ ] Test large crawls (1000+ requests)
- [ ] Test deep nesting (5+ levels)
- [ ] Test parallel execution (100+ concurrent)
- [ ] Test memory usage over time

---

## Success Criteria

### Developer Experience

- [ ] Developer can understand execution flow at a glance
- [ ] All context mutations are visible
- [ ] Merge targets always clear
- [ ] Template variable usage visible
- [ ] Parallel execution easy to understand

### Performance

- [ ] Profiler overhead < 10%
- [ ] Extension responsive with 10,000+ events
- [ ] Snapshots load in < 100ms
- [ ] Diffs compute in < 50ms
- [ ] Memory usage < 3x raw data

### Completeness

- [ ] All operation types profiled
- [ ] All contexts visible at all times
- [ ] All mutations tracked
- [ ] All template evaluations shown
- [ ] All merge operations clear

---

## Future Enhancements (Post-Plan)

### Advanced Features

1. **Breakpoints**: Pause execution at specific events
2. **Step Through**: Execute one event at a time
3. **Replay**: Re-execute with modified context
4. **Export**: Save profiler data for later analysis
5. **Compare Runs**: Diff two crawl executions
6. **Search**: Find events by criteria (URL, context, etc)
7. **Filters**: Show only errors, only merges, etc
8. **Bookmarks**: Mark interesting events
9. **Notes**: Add developer notes to events
10. **Sharing**: Share profiler sessions with team

### Analytics

1. **Performance metrics**: Request durations, bottlenecks
2. **Context growth**: Track context size over time
3. **Merge patterns**: Most common merge operations
4. **Error analysis**: Where errors occur most
5. **Rate limiting impact**: How much throttling affects time

---

## Migration Path

### Backward Compatibility

- [ ] Keep old event format working (don't break existing)
- [ ] Add feature flag for new profiler
- [ ] Gradual migration: old events + new events
- [ ] Eventually deprecate old format

### Migration Steps

1. **Phase 1-3**: Run both old and new profiler
2. **Phase 4-6**: Default to new, allow fallback to old
3. **Phase 7-10**: New only, remove old code

---

## Notes for Future Implementation

### Important Considerations

1. **Context is the core concept** - Everything revolves around context map visibility
2. **All contexts must be accessible** - Not just current, but entire map at any snapshot
3. **Snapshots are expensive** - Only snapshot on mutations, not every event
4. **Parallelism is complex** - Need correlation IDs and proper event ordering
5. **Diffs are valuable** - Worth the computation cost for UX improvement
6. **Performance matters** - Profiler shouldn't slow down crawl significantly

### Common Pitfalls to Avoid

1. Don't deep copy on every event (use references)
2. Don't forget to show which context was modified
3. Don't hide template variable resolution
4. Don't make events too generic (be specific)
5. Don't forget parallel execution ordering
6. Don't snapshot unchanged contexts (waste)

### Key Success Factors

1. Make context map state always visible
2. Show clear before/after diffs
3. Use semantic event names
4. Preserve execution hierarchy
5. Handle parallelism gracefully
6. Keep profiler overhead low

---

## Document History

- **2025-11-04**: Initial plan created
- Status: Planning phase, not yet implemented
- Next step: Implement Phase 1 (Enhanced Event Types)

---

## Questions to Answer Before Implementation

1. Should snapshots be stored in memory or written to disk?
   - **Recommendation**: Memory for now, with configurable limit

2. How to handle very large contexts (100MB+ responses)?
   - **Recommendation**: Paginate large arrays, add size warnings

3. Should diffs be computed in Go or TypeScript?
   - **Recommendation**: TypeScript (better diff libraries, on-demand)

4. How to correlate events across parallel threads?
   - **Recommendation**: Use execution batch ID + iteration number

5. Should timeline view be real-time or post-execution?
   - **Recommendation**: Post-execution for accuracy

---

## Contact & Feedback

When implementing this plan:
- Revisit phases in order
- Test each phase thoroughly before moving on
- Adjust plan based on learnings
- Keep performance in mind at every step
- Always ask: "Can the developer understand what happened?"

**End of Plan Document**
