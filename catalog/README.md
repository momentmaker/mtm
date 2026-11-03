# mtm's app catalog

One TOML file per app, named after its `id`. mtm builds every file here into its binary. The tests in `internal/catalog` check each one, and a file they reject fails CI.

```toml
id = "raycast"                 # lowercase words joined by hyphens; must match the file name
name = "Raycast"               # as the app names itself
bundle_id = "com.raycast.macos"

[[permissions]]                # privacy permissions the app needs to work
service = "Accessibility"      # a name from internal/catalog/services.go
# client = "com.example.helper"  # only when macOS records the grant for a helper, not the app

[license]
reactivate = true              # the license must be entered again on a new Mac
note = "Enter your license again if it asks."

notes = ["Sign in to your account again if it asks."]   # extra checklist steps

[state]
volatile = ["~/Library/Application Support/Raycast/logs/"]   # paths the app deletes on its own
```

mtm turns these into checklist steps on the new Mac:
- one per permission, unless it can read the old Mac's privacy permissions directly;
- one for a license to reactivate;
- one per note.

Volatile paths that are missing after Migration Assistant are reported as info, not warnings.

## Adding an app

1. Find its bundle id: `plutil -extract CFBundleIdentifier raw /Applications/<App>.app/Contents/Info.plist`.
2. Add `<id>.toml` with only the fields you know to be true.
3. Run `go test ./internal/catalog/`.

## Credits

Several apps' names and bundle ids were first taken from the definitions of [tape](https://github.com/vitorgalvao/tape) by Vítor Galvão, under the BSD 2-Clause License:

```
Copyright (c) 2022, Vítor Galvão
Copyright (c) 2022, Vítor Galvão
All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```
