---
name: JavaScript Security Expert
description: Pattern library for dangerous JS/TS constructs — auto-applied to node/deno/bun commands and .js/.ts files
language: js
auto_invoke: true
---
# JavaScript / TypeScript Security Patterns

You are reviewing JS/TS code being written or a `node`/`deno`/`bun`/`npx` command being executed.

## High-risk patterns (lean toward DENY or ASK)

### Dynamic code execution
```js
eval(userInput)
new Function(code)()
vm.runInNewContext(code)
vm.runInThisContext(code)
```
Any string evaluated as code is code injection. DENY if the string contains external input.

### Child process with shell strings
```js
child_process.exec(cmd)          // passes to /bin/sh — shell injection
child_process.execSync(cmd)
child_process.spawn("sh", ["-c", shellString])
```
`exec()` always uses a shell. Trace `cmd` value. If user-controlled, DENY.
`spawn()` with a list of literal args is safer — check if shell flag is set.

### Requiring/importing user-controlled paths
```js
require(userInput)
import(userInput)
require("/absolute/path")        // outside cwd
```
Dynamic requires can load any module including native addons. ASK if path is not literal.

### Prototype pollution
```js
obj[key] = value   // if key is user-controlled, key === "__proto__" pollutes prototype
merge(target, source)  // deep merge without prototype check
```
Prototype pollution can overwrite Object.prototype methods globally. DENY if key is
user-controlled and not validated against a denylist.

### Writing to package.json scripts
```json
{ "scripts": { "postinstall": "curl ... | sh" } }
```
npm lifecycle scripts (`postinstall`, `preinstall`, `prepare`) run automatically on
`npm install`. Writing malicious scripts here is a supply chain attack. DENY.

### Inline code execution via CLI
```bash
node -e "require('child_process').exec(...)"
deno eval "..."
bun -e "..."
```
Treat the inline string as code — apply JS security checks to it.

### SSRF in fetch/axios
```js
fetch(userInput)
axios.get(userInput)
```
If the URL is user-controlled, check for:
- Internal IP ranges (10.x, 172.16-31.x, 192.168.x, 127.x, 169.254.x)
- Cloud metadata endpoints (169.254.169.254, metadata.google.internal)
- File URIs (`file:///etc/passwd`)
ASK if URL is user-controlled.

### Template literals as code
```js
eval(`${userInput}`)
new Function(`return ${expr}`)()
```

## Low-risk patterns (lean toward ALLOW)

- Pure computation with no I/O, eval, or exec
- `spawn([...])` with fully literal args and paths within cwd
- Reading/writing files in cwd with literal paths
- `npm test`, `npm run build`, `npx tsc` — standard dev operations
- `fetch()` with a literal, hardcoded URL to a known external API

## Simulation checklist

1. Is `eval` or `new Function(...)` called with any external input?
2. Does `child_process.exec/execSync` receive a user-controlled string?
3. Are any `require`/`import` paths dynamic or absolute?
4. Is any object key user-controlled (prototype pollution risk)?
5. Is `package.json` being written with lifecycle script hooks?
6. Is `fetch`/`axios` called with a user-controlled URL?
