# linenoise fork

Upstream: https://github.com/antirez/linenoise

Base commit: `a473823d74b93eab2ba83480df16ed37617493f2`

The source keeps the upstream BSD license. The orc fork adds:

- nonblocking history and Escape hooks
- multiline input with explicit soft breaks
- UTF-8-aware multiline redraw and cursor geometry
- safe hide and in-place show operations
- full CSI parameter parsing, including Shift+Enter
- `TCSANOW` terminal mode changes to keep type-ahead
