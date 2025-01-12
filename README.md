# slog: Systemd journal handler

## Usage

> [!NOTE]
> Journald only supports keys of the form `^[A-Z_][A-Z0-9_]*$`. Any other keys will be silently dropped.

```go
h := slogjournal.NewHandler(nil)
log := slog.New(h)
log.Info("Hello, world!", "EXTRA_KEY", "5")
log.Info("Hello, world!", slog.Group("HTTP", "METHOD", "put", "URL", "http://example.com"))
```


> [!CAUTION]
> This is pre-release software. No releases have been tagged yet.