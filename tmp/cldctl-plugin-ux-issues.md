# cldctl plugin discovery & display UX issues

## Issue 1: `cldctl catalog list` silently hides locally-built pre-release providers

### Problem

When the only available version of a locally-built provider is a pre-release
(e.g. `0.5.0-dev`), `cldctl catalog list --kind provider` returns no results
for that artifact — no entry, no warning, no hint it exists.

```sh
$ cldctl catalog list --kind provider
╭──── cldctl ────╮
│  name  tag    │
│  ───────────  │
│  oci   0.6.3  │
╰────────────────╯
```

`exec@0.5.0-dev` is in the local catalog but completely invisible. There is no
indication that it was filtered out, and no suggestion to add `--pre-release`.

### Expected behaviour

Pre-release artifacts should appear in `catalog list` output, either:
- **Always**, with a `(pre-release)` or `*` marker in the table, or
- **With a footer note**: "N artifact(s) hidden — add --pre-release to show them"

Silently dropping them makes local development workflows opaque. A developer who
runs `cldctl build plugin` and then `cldctl catalog list` should be able to see
what they just built.

### Workaround

```sh
cldctl catalog list --name exec --all-versions
# or
cldctl catalog list --kind provider --pre-release --all-versions
```

### Reproduction

```sh
cldctl build plugin --force --name exec --kind provider --version 0.5.0-dev \
  --platform windows/amd64=./dist/scafctl-plugin-exec.exe

cldctl catalog list --kind provider
# exec does not appear — but it is in the catalog
```

---

## Issue 2: `cldctl plugins list` — two display problems

### Problem A: `path` column shown in table view

The `path` column shows a long filesystem path that is always truncated
(`C:\Users\abaker9\AppData\Local\cache\...`) in table view, which wastes
column space and adds no useful information at a glance.

**Current output:**

```
╭────────────────────────────────────────────────────────────────────────────╮
│#   name                 path                       platform       size     │
│──────────────────────────────────────────────────────────────────────────  │
│1   auth-handler-entra   C:\Users\...\cache\...     windows/amd64  1.68e+07 │
│2   exec                 C:\Users\...\cache\...     windows/amd64  1.58e+07 │
╰────────────────────────────────────────────────────────────────────────────╯
```

The path is always the same cache directory prefix — it is not actionable in
table view and should be hidden by default. It could be surfaced in `--output
list` or `--output json` for users who need it.

### Problem B: `size` shown in scientific notation

File sizes are displayed as raw byte counts in scientific notation
(`1.6792576e+07`) which is unreadable at a glance.

**Current:**
```
1.6792576e+07
```

**Expected:**
```
16.0 MB
```

Sizes should be formatted as human-readable values (KB / MB / GB) in all
table/list output formats. Raw bytes can remain in `--output json`.

### Suggested table layout

```
╭────────────────────────────────────────────────────╮
│#   name                 platform       size   ver  │
│──────────────────────────────────────────────────  │
│1   auth-handler-entra   windows/amd64  16 MB  0.1.1│
│2   exec                 windows/amd64  15 MB  0.5.0-dev│
╰────────────────────────────────────────────────────╯
```

### Full repro output (actual)

```
╭─────────────────────────────────────────────────────────────────────────────────────╮
│#     name                 path                      platform       size           version │
│─────────────────────────────────────────────────────────────────────────────────────     │
│1     auth-handler-entra   C:\...\cache\...          windows/amd64  1.6792576e+07  0.1.1  │
│2     auth-handler-gcp     C:\...\cache\...          windows/amd64  1.686016e+07   0.1.1  │
│3     auth-handler-github  C:\...\cache\...          windows/amd64  1.6818176e+07  0.1.5  │
│4     auth-handler-github  C:\...\cache\...          windows/amd64  1.68192e+07    0.1.6  │
│5     directory            C:\...\cache\...          windows/amd64  1.4246912e+07  0.1.0  │
│6     env                  C:\...\cache\...          windows/amd64  1.415168e+07   0.1.0  │
│7     exec                 C:\...\cache\...          windows/amd64  1.5803392e+07  0.2.0-dev│
│8     exec                 C:\...\cache\...          windows/amd64  1.5732224e+07  0.4.0  │
│9     git                  C:\...\cache\...          windows/amd64  1.4319104e+07  0.2.0  │
│10    github               C:\...\cache\...          windows/amd64  1.6443392e+07  0.3.0  │
│11    hcl                  C:\...\cache\...          windows/amd64  1.6262656e+07  0.1.0  │
│12    identity             C:\...\cache\...          windows/amd64  1.4208e+07     0.2.0  │
│13    metadata             C:\...\cache\...          windows/amd64  1.4148608e+07  0.2.0  │
│14    oci                  C:\...\cache\...          windows/amd64  1.7406464e+07  0.6.3  │
│15    secret               C:\...\cache\...          windows/amd64  1.4168064e+07  0.1.0  │
│16    sleep                C:\...\cache\...          windows/amd64  1.4140928e+07  0.1.0  │
╰─────────────────────────────────────────────────────────────────────────────────────╯
```

---

## Issue 3: `cldctl plugins list` vs `cldctl catalog list` — confusing distinction

A developer who runs `cldctl build plugin` and then tries to find their artifact
with `cldctl plugins list` will not see it there. `plugins list` shows only the
**remote download cache** — binaries fetched from OCI registries. Locally-built
artifacts are in the **catalog** and only visible via `cldctl catalog list`.

This is a meaningful distinction but it is not documented anywhere in the command
output or help text.

### Suggested fix

Add a note to `cldctl plugins list --help` and/or the command output footer:

> "Showing remote plugin cache only. To see locally-built artifacts, run:
> `cldctl catalog list --kind provider --pre-release --all-versions`"

---

## Environment

- OS: Windows 11 (amd64)
- cldctl: v1.0.0-alpha.1+dirty
