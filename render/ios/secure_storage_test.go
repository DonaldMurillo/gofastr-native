package ios

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/securestore"
)

func TestSecureStorageSwiftContract(t *testing.T) {
	requireSwift(t)
	directory := t.TempDir()
	sourcePath := swiftSources(t, directory, Sources, "GofastrSecureStorage.swift")[0]
	fixture := map[string]any{
		"requests": []securestore.Request{
			{Operation: securestore.Status},
			{Operation: securestore.Put, Key: "token", Protection: securestore.Device, Value: []byte{0, 1, 255}},
			{Operation: securestore.Get, Key: "token", Protection: securestore.Biometric, Reason: "Read private token"},
			{Operation: securestore.Authenticate, Protection: securestore.Biometric, Reason: "Unlock notes"},
			{Operation: securestore.Delete, Key: "token", Protection: securestore.Biometric},
		},
		"result": securestore.Result{Value: []byte{0, 1, 255}, Found: true},
	}
	content, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(directory, "wire.json")
	if err := os.WriteFile(fixturePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	compileRunChecks(t, directory, "checks", secureStorageChecks,
		"secure storage contract compile", "secure storage contract", []string{sourcePath}, []string{fixturePath})
}

const secureStorageChecks = `
import Foundation
struct Fixture: Decodable { let requests: [MobileSecureStorageRequest]; let result: MobileSecureStorageResult }
@main struct Checks {
  static func main() throws {
    let fixture = try JSONDecoder().decode(Fixture.self, from: Data(contentsOf: URL(fileURLWithPath: CommandLine.arguments[1])))
    for request in fixture.requests { try request.validate() }
    precondition(fixture.result.found && fixture.result.value == Data([0, 1, 255]))
    precondition(fixture.result.wire["value"] as? String == "AAH/")
    precondition(MobileSecureStorageResult().wire["value"] == nil)
    let invalid = [
      #"{"operation":"put","key":"token","protection":"device"}"#,
      #"{"operation":"get","key":"token","protection":"biometric"}"#,
      #"{"operation":"get","key":"../token","protection":"device"}"#,
      #"{"operation":"get","key":"token","protection":"device","value":"AAH/"}"#,
      #"{"operation":"authenticate","protection":"device","reason":"Unlock"}"#,
      #"{"operation":"status","key":"token"}"#
    ]
    for wire in invalid {
      let request = try JSONDecoder().decode(MobileSecureStorageRequest.self, from: Data(wire.utf8))
      do { try request.validate(); fatalError("Accepted invalid secure storage request") } catch {}
    }
    var oversized = fixture.requests[1]
    oversized.value = Data(repeating: 0, count: 16_385)
    do { try oversized.validate(); fatalError("Accepted oversized secret") } catch {}
    var control = fixture.requests[2]
    control.reason = "Read\nsecret"
    do { try control.validate(); fatalError("Accepted control in authentication reason") } catch {}
  }
}
`
