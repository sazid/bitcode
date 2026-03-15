---
name: Python Security Expert
description: Pattern library for dangerous Python constructs — auto-applied to Python commands and .py files
language: python
auto_invoke: true
---
# Python Security Patterns

You are reviewing Python code being written or executed. Apply these checks.

## High-risk patterns (lean toward DENY or ASK)

### Dynamic code execution
```python
eval(user_input)           # arbitrary code execution
exec(compiled_code)        # arbitrary code execution
compile(src, ...) + exec() # indirect exec
```
Any `eval`/`exec` receiving external or environment-controlled input is code injection. DENY.

### Subprocess with shell=True
```python
subprocess.run(cmd, shell=True)
subprocess.Popen(cmd, shell=True)
os.system(cmd)
os.popen(cmd)
```
When `shell=True`, `cmd` is passed to `/bin/sh -c`. Trace the value of `cmd`.
If `cmd` contains user input or interpolated variables, treat as shell injection. ASK or DENY.

### Pickle deserialization
```python
pickle.loads(untrusted_data)
pickle.load(open("untrusted.pkl"))
```
Pickle can execute arbitrary code during deserialization. ASK if source is not local/trusted.

### Unsafe YAML loading
```python
yaml.load(data)                   # dangerous — use yaml.safe_load
yaml.load(data, Loader=yaml.FullLoader)  # also dangerous
```
Use `yaml.safe_load()`. Flag any `yaml.load()` without `Loader=yaml.SafeLoader`. ASK.

### Template injection
```python
template.format(**user_data)      # can access dunder attributes
f"SELECT * FROM {table}"          # SQL injection if table is user-controlled
```

### Import hijacking
Writing a file named the same as a stdlib module (e.g., `os.py`, `json.py`) in cwd can
shadow the real module when Python resolves imports. Flag if the file being written matches
a stdlib module name. ASK.

### Dynamic imports of dangerous modules
```python
__import__("ctypes")
__import__("cffi")
importlib.import_module("mmap")
```
These grant access to raw memory and native code execution. ASK.

### Network with credential leakage
```python
requests.get(url, headers={"Authorization": secret})
urllib.request.urlopen(url)   # check if URL or headers contain secrets
```
Flag if credentials, tokens, or env vars containing secrets appear in request headers/body/URL.

## Low-risk patterns (lean toward ALLOW)

- Pure computation: no I/O, no subprocess, no network
- File reads within cwd using literal paths
- Standard library usage without shell=True
- `subprocess.run([...])` (list form, no shell) with literal args and path within cwd
- Writing `.py` files with no dangerous patterns above

## Simulation checklist

1. Is `eval`/`exec` called with any external or env-derived value?
2. Is `subprocess` used with `shell=True`? What is the shell string?
3. Is pickle/YAML loading from an external source?
4. Does any network call include credentials or send data to an external host?
5. Is a file being written that shadows a stdlib module?
