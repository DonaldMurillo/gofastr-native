package ios

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/widgets"
)

func TestWidgetSwiftContract(t *testing.T) {
	requireSwift(t)
	directory := t.TempDir()
	sourcePath := swiftSources(t, directory, Sources, "GofastrWidgets.swift")[0]
	fixture, err := json.Marshal([]widgets.Request{
		{Operation: widgets.Publish, Snapshot: &widgets.Snapshot{Title: "Recent notes", Text: "Published by Go", Route: "/notes?filter=recent%20notes", Items: []widgets.Item{{ID: "1", Title: "Shopping", Subtitle: "Milk\nBread"}}}},
		{Operation: widgets.Clear}, {Operation: widgets.Status},
	})
	if err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(directory, "widget-wire.json")
	if err := os.WriteFile(fixturePath, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	compileRunChecks(t, directory, "widget-checks", widgetSwiftChecks,
		"compile Widget DTOs", "execute Widget DTOs", []string{"-O", sourcePath}, []string{fixturePath})
}

const widgetSwiftChecks = `
import Foundation
@main enum WidgetChecks {
    static func main() throws {
        let data = try Data(contentsOf: URL(fileURLWithPath: CommandLine.arguments[1]))
        let requests = try JSONDecoder().decode([MobileWidgetRequest].self, from: data)
        precondition(requests.count == 3)
        for request in requests { try request.validate() }
        precondition(requests[0].snapshot?.items?.first?.subtitle == "Milk\nBread")
        for route in ["https://evil.test", "//evil.test", "/notes/../private", "/%2e%2e/private", "/notes%2fprivate", "/notes%0aother", "/notes?q=%0d", "/notes%5cprivate", "/notes#secret", "/notes?bad=%", "/white space"] {
            precondition(!MobileWidgetSnapshot.localRoute(route), route)
        }
        for route in ["/", "/notes/1", "/notes?query=hello%20world"] { precondition(MobileWidgetSnapshot.localRoute(route), route) }
        let invalid = MobileWidgetSnapshot(title: "Notes", text: nil, route: "/notes", items: [MobileWidgetItem(id: "1", title: "One", subtitle: nil), MobileWidgetItem(id: "1", title: "Two", subtitle: nil)])
        do { try invalid.validate(); fatalError("accepted duplicate widget items") } catch MobileWidgetFailure.invalid { }
        let state = try JSONEncoder().encode(MobileWidgetState(published: false))
        precondition(String(data: state, encoding: .utf8) == "{\"published\":false}")
    }
}
`
