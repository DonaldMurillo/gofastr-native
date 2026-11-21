package ios

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/refresh"
)

func TestBackgroundRefreshSwiftContract(t *testing.T) {
	requireSwift(t)
	directory := t.TempDir()
	sourcePath := swiftSources(t, directory, Sources, "GofastrBackgroundRefresh.swift")[0]
	fixture, err := json.Marshal([]refresh.Request{
		{Operation: refresh.Schedule, IntervalMinutes: refresh.MinIntervalMinutes}, {Operation: refresh.Schedule, IntervalMinutes: refresh.MaxIntervalMinutes},
		{Operation: refresh.Cancel}, {Operation: refresh.Status},
	})
	if err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(directory, "refresh-wire.json")
	if err := os.WriteFile(fixturePath, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	compileRunChecks(t, directory, "refresh-checks", refreshSwiftChecks,
		"compile background refresh DTOs", "execute background refresh DTOs", []string{"-O", sourcePath}, []string{fixturePath})
}

const refreshSwiftChecks = `
import Foundation
@main enum RefreshChecks {
    static func main() throws {
        let data = try Data(contentsOf: URL(fileURLWithPath: CommandLine.arguments[1]))
        let requests = try JSONDecoder().decode([MobileRefreshRequest].self, from: data)
        precondition(requests.count == 4)
        for request in requests { try request.validate() }
        for invalid in [#"{"operation":"schedule"}"#, #"{"operation":"schedule","intervalMinutes":14}"#, #"{"operation":"schedule","intervalMinutes":10081}"#,
                        #"{"operation":"cancel","intervalMinutes":15}"#, #"{"operation":"run"}"#] {
            let request = try JSONDecoder().decode(MobileRefreshRequest.self, from: Data(invalid.utf8))
            precondition((try? request.validate()) == nil, invalid)
        }
        let state = String(decoding: try JSONEncoder().encode(MobileRefreshState(scheduled: false)), as: UTF8.self)
        precondition(state == #"{"scheduled":false}"#, state)
        let defaults = UserDefaults(suiteName: "gofastr.refresh.checks")!
        defaults.removePersistentDomain(forName: "gofastr.refresh.checks")
        let preference = MobileRefreshPreference(defaults: defaults)
        precondition(preference.intervalMinutes == nil)
        preference.save(60)
        precondition(preference.intervalMinutes == 60)
        defaults.set(5, forKey: MobileRefreshPreference.key)
        precondition(preference.intervalMinutes == nil)
        preference.save(nil)
        precondition(defaults.object(forKey: MobileRefreshPreference.key) == nil)
        for valid in ["dev.gofastr.features.refresh", "a-b.c"] { precondition(MobileRefreshIdentifier.valid(valid), valid) }
        for invalid in ["", ".refresh", "refresh.", "a..b", "a b", "a/b", String(repeating: "a", count: 201)] { precondition(!MobileRefreshIdentifier.valid(invalid), invalid) }
    }
}
`
