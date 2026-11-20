# Choose a contact on Android

Register `extension/contacts.Definition()` with `nativeui.NewWithExtensions`.
Call `contacts.Pick(contacts.Request{Title: "Choose contact", Kind: contacts.Phone})`
(or `contacts.Email`) from an action and return the operation in a
`nativeui.FileRequest`. Read the accepted document in the Files continuation
with `request.Contact(document)`.

The compiled Android adapter uses the system contact row picker. It reads only
the returned URI on a worker, never the full address book, and requests no
READ_CONTACTS permission. Cancellation uses the framework's cancelled outcome;
security errors are denied and other provider errors are failed. Availability
is determined without prompting by resolving the pick intents.

Results are `Contact.json`, `application/json`, at most 4 KiB. Go rejects unknown
JSON fields, trailing data, control characters, overlong fields and a detail
that does not match the requested kind. Names are bounded to 512 bytes, phone
numbers to 256 and email addresses to 320. The unrequested detail must be empty.
No provider identifier or OS path is returned.

The Gallery Device tab has a Contacts demo with a **Choose contact** button.
Its selected name and number remain in memory only. iOS and WebView adapters
are not implemented; those renderers cannot open the capability-gated demo.

Android 14 x86-64 emulator acceptance covered a seeded system contact, phone
selection, display of its exact name and number, Back cancellation retaining
the selection, and reopening after cancellation. The crash buffer and filtered
runtime error log were empty. The debug APK installed successfully and passed
signature verification. Run the Gallery-only flow with:

```sh
python scripts/android_smoke.py --contacts --serial emulator-5554 --package dev.gofastr.gallerynative --out dist/contacts-acceptance
```

The script adds a uniquely named synthetic contact and leaves existing app data
and contacts intact. Screenshots and UI XML cover before, picker, after-select
and after-cancel states. The Notes `--services` flow remains independent.

Email selection, denied access, physical providers/devices, TalkBack and IME
acceptance: NOT RUN. Emulator acceptance does not establish those gates.
