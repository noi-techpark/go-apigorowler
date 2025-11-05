# ApiGorowler Binaries

This directory contains pre-built binaries for different platforms.

## Available Binaries

- **linux/amd64**: `apigorowler-linux-amd64` (9.76 MB)
- **linux/arm64**: `apigorowler-linux-arm64` (9.31 MB)
- **darwin/amd64**: `apigorowler-darwin-amd64` (10.04 MB)
- **darwin/arm64**: `apigorowler-darwin-arm64` (9.53 MB)
- **windows/amd64**: `apigorowler-windows-amd64.exe` (10.08 MB)
- **windows/arm64**: `apigorowler-windows-arm64.exe` (9.36 MB)

## Usage

The extension automatically selects the appropriate binary for your platform.

You can also run the binary directly from the command line:

```bash
./apigorowler-<platform>-<arch> -config path/to/config.apigorowler.yaml -profiler
```

### Flags

- `-config <path>`: Path to configuration file (required)
- `-profiler`: Enable profiler output (JSON per step)
- `-validate`: Only validate configuration without running

## Building from Source

To rebuild binaries:

```bash
npm run build:binary           # Build for current platform
npm run build:binary:all        # Build for all platforms
```
