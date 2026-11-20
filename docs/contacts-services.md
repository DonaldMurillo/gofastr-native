# Contact picker

iOS provides an explicitly initiated Contacts picker through `contacts/v1`.
Android and plain browsers do not advertise support. The default native and
WebView iOS hosts share the same service runner.

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

In Notes, open or create a note and choose **Pick contact** or **Pick contacts**
from its actions. Go runtime tests, executable Swift wire checks, and the
browser test `TestNotesContactsPickerWebView` cover the contract and injected
completed/cancelled/multiple outcomes. Native picker GUI acceptance: NOT RUN.
A successful build or launch does not establish picker acceptance.
