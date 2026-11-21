package ios

import "testing"

func TestScannerSwiftContract(t *testing.T) {
	requireSwift(t)
	directory := t.TempDir()
	sources := swiftSources(t, directory, Sources, rendererSourceFiles...)
	compileRunChecks(t, directory, "scanner-checks", scannerChecks,
		"scanner wire checks do not compile", "scanner validation regression", append([]string{"-O"}, sources...), nil)
}

const scannerChecks = `
import Foundation
@main struct Checks {
    static func main() throws {
        let decoder = JSONDecoder()
        let request = try decoder.decode(MobileServiceRequest.self, from: Data(#"{"id":"scan","scan":{"title":"Scan","symbologies":["qr"]}}"#.utf8))
        try request.validate()
        precondition(request.scan?.selected == ["qr"])
        try MobileScanRequest(title: "Scan", symbologies: []).validate()
        for wire in [#"{"title":""}"#, #"{"title":"Scan","symbologies":["qr","qr"]}"#, #"{"title":"Scan","symbologies":["unknown"]}"#] {
            do { try decoder.decode(MobileScanRequest.self, from: Data(wire.utf8)).validate(); preconditionFailure("accepted invalid scan") }
            catch {}
        }
        let result = MobileScanResult(value: "fixture", symbology: "qr")
        precondition(MobileServiceResult(status: "completed", scan: result).wire["scan"] != nil)
        precondition(MobileServiceResult(status: "cancelled", scan: result).wire["scan"] == nil)
    }
}
`
