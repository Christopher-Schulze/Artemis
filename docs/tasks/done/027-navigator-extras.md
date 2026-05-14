# TASK 027: Navigator extras (UAData / Clipboard / Geolocation / Permissions / Service Worker)

## Done

- [x] `navigator.userAgentData` with brands, mobile, platform, getHighEntropyValues, toJSON
- [x] `navigator.clipboard.{writeText, readText, write, read}` (in-context state, not OS clipboard)
- [x] `navigator.geolocation.{getCurrentPosition, watchPosition, clearWatch}` (rejects with permission denied)
- [x] `navigator.permissions.query` (always returns 'denied' so feature detection passes but sensitive ops fail predictably)
- [x] `navigator.serviceWorker` stub (register rejects, getRegistrations returns [])
- [x] `navigator.hardwareConcurrency`, `deviceMemory`, `maxTouchPoints` properties

## Notes

These stubs let pages that feature-detect these APIs proceed without crashing. Behavior is "nothing actually happens": clipboard works in-context, geolocation refuses, permissions always denied. Real implementations need OS integration which is out of scope for an embedded headless engine.
