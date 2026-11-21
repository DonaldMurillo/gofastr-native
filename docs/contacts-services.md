# Contact picker

iOS and the native Android renderer provide an explicitly initiated contact
picker through `contacts/v1`. The Android WebView renderer and plain browsers
do not advertise support. The default native and WebView iOS hosts share the
same service runner.

Return a `nativeui.ServiceRequest{ID: "contacts", Contacts: &contacts.Request{
Fields: []contacts.Field{contacts.Phone}}}` from an action and register a
`ServiceRespond` under that ID in the route's `Services` map.
Set `Multiple: true` for multiple selection. A completed result carries
`ServiceResult.Contacts`; cancellation carries no contact payload.

Names and the opaque `CNContact.identifier` are always returned. Optional
fields are `phone`, `email`, `postal`, and `organization`. Unknown and
duplicate fields are rejected. Only requested fields may be returned.
Selection is limited to 50 contacts (exactly one for single selection),
20 values per list, 512 UTF-8 bytes per name, identifier, organization,
label or value, and 2048 bytes per formatted postal address. Postal addresses
permit tabs and newlines; other strings reject controls. The iOS adapter
truncates content to these limits and validates before settling once.

The picker uses `CNContactPickerViewController`, not full address-book access.
It does not request Contacts authorization and does not require
`NSContactsUsageDescription`. Selected contact data is returned to the
application; Notes only displays the selected names and first phone numbers
in a transient announcement and does not save them or modify note text.
Treat identifiers as opaque private data, not authorization to read contacts.

## Android

The native Android renderer advertises `contacts/v1` when the system phone or
email row picker resolves; it checks this without prompting. It serves single
selection of exactly one field, `phone` or `email`. Any other request
(`Multiple: true`, no fields, more than one field, `postal` or
`organization`) settles as `unsupported` without opening a picker.

The adapter opens `ACTION_PICK` on the phone or email data rows and reads only
the row URI the picker grants, on a worker. It does not request
`READ_CONTACTS`; generated native manifests declare the two picker intents as
package-visibility queries. Cancelling the picker settles `cancelled`, a
`SecurityException` settles `denied`, and other provider errors settle
`failed`.

The result has one contact. Android exposes only the row's display name, so
`GivenName` carries the whole display name and `FamilyName` is empty. `ID` is
the contact's lookup key, or the data row ID when the lookup key is missing or
over the limit. `Phones` or `Emails` holds the one selected value with its
localized type label. Text over 512 bytes or containing control characters
fails the request instead of being truncated.

The Gallery Device tab has a Contacts demo with a **Choose contact** button
that requests one phone number and displays the selected name and number. The
selection stays in memory only. Run its emulator flow with:

```sh
python scripts/android_smoke.py --contacts --serial emulator-5554 --package dev.gofastr.gallerynative --out dist/contacts-acceptance
```

The script adds a uniquely named synthetic contact and leaves existing app data
and contacts intact. Screenshots and UI XML cover before, picker, after-select
and after-cancel states.

## Checks

In Notes, open or create a note and choose **Pick contact** or **Pick contacts**
from its actions. Go runtime tests, executable Swift wire checks, and the
browser test `TestNotesContactsPickerWebView` cover the contract and injected
completed/cancelled/multiple outcomes. iOS native picker GUI acceptance: NOT RUN.
A successful build or launch does not establish picker acceptance.

On Android, `TestAndroidContactsWire` checks the adapter's JSON against the Go
validation, the Android SDK compile test builds the renderer, and
`TestNativeAndroidContactsPickerManifest` checks the picker queries and the
absent `READ_CONTACTS` permission. ANDROID_EMULATOR_RESULT
Email selection, denied access, physical devices and providers, TalkBack and
IME acceptance: NOT RUN.
