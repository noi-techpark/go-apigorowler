# Changelog

All notable changes to the ApiGorowler VSCode extension will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Initial release of ApiGorowler VSCode extension
- YAML syntax highlighting for `.apigorowler.yaml` files
- JSON Schema validation with IntelliSense
- 25+ code snippets for common patterns:
  - Basic configurations
  - Request steps with various options
  - ForEach steps (serial and parallel)
  - Pagination patterns (URL, offset, cursor)
  - Authentication configurations
  - Merge strategies
- Live execution and debugging
- Execution Steps Explorer with hierarchical view
- Step details panel with before/after data comparison
- Auto-validation on save (configurable)
- Output channel integration
- Command palette commands
- Editor title buttons (Run, Stop, Validate)
- Export execution steps to JSON
- Diagnostics integration for validation errors

### Configuration Options
- `apigorowler.executable.path` - Path to Go executable
- `apigorowler.autoValidate` - Auto-validate on save
- `apigorowler.autoRun` - Auto-run on save
- `apigorowler.maxOutputSize` - Max output lines
- `apigorowler.collapseSteps` - Collapse steps by default

### Commands
- `apigorowler.run` - Run configuration
- `apigorowler.stop` - Stop execution
- `apigorowler.debug` - Debug with profiler
- `apigorowler.validateConfig` - Validate without running
- `apigorowler.exportSteps` - Export step tree to JSON
- `apigorowler.refreshSteps` - Refresh steps view
- `apigorowler.collapseAllSteps` - Collapse all steps
- `apigorowler.showStepDetails` - Show step details panel

## [0.1.0] - TBD

Initial release - see Unreleased section for features.
