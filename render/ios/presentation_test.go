package ios

import (
	"testing"
)

func TestPresentationWireAndSelectionState(t *testing.T) {
	requireSwift(t)
	directory := t.TempDir()
	modelPath := swiftSources(t, directory, Sources, "GofastrPresentation.swift")[0]
	compileRunChecks(t, directory, "presentation-checks", presentationChecks,
		"presentation model checks do not compile", "presentation wire or local selection regression", []string{modelPath}, nil)
}

const presentationChecks = `
import Foundation

@main
enum PresentationChecks {
    static func main() throws {
        let decoder = JSONDecoder()
        let alertJSON = #"{"id":"app-continuation","kind":"alert","title":"Confirm","choices":[{"id":"approve","label":"Continue","disabled":false},{"id":"cancel","label":"Cancel","role":"cancel","disabled":false},{"id":"delete","label":"Delete","role":"destructive","disabled":true}],"dismissible":false,"commit":false}"#
        let alert = try decoder.decode(MobilePresentation.self, from: Data(alertJSON.utf8))
        precondition(alert.id == "app-continuation")
        precondition(alert.message == nil && alert.selection == nil)
        precondition(!alert.dismissible && !alert.commit)
        precondition(alert.choices[0].role == nil)
        precondition(alert.choices[1].role == "cancel")
        precondition(alert.choices[2].role == "destructive" && alert.choices[2].disabled)
        precondition(MobilePresentationSelectionState(nil).selected.isEmpty)

        for kind in ["alert", "confirmation", "menu", "dialog", "sheet"] {
            let changed = alertJSON.replacingOccurrences(of: #""kind":"alert""#, with: "\"kind\":\"\(kind)\"")
            let decoded = try decoder.decode(MobilePresentation.self, from: Data(changed.utf8))
            precondition(decoded.kind == kind)
        }

        let multipleJSON = #"{"options":[{"id":"first","label":"First","detail":"A detail","disabled":false},{"id":"second","label":"Second","disabled":false},{"id":"locked","label":"Locked","disabled":true}],"selected":["second"],"multiple":true,"min":1,"max":1}"#
        let multiple = try decoder.decode(MobilePresentationSelection.self, from: Data(multipleJSON.utf8))
        var state = MobilePresentationSelectionState(multiple)
        precondition(state.selected == ["second"])
        state.toggle(multiple.options[0])
        precondition(state.selected == ["first", "second"], "Go, not UIKit, must enforce min/max")
        state.toggle(multiple.options[2])
        precondition(state.selected == ["first", "second"], "disabled options cannot change")
        state.toggle(multiple.options[1])
        state.toggle(multiple.options[0])
        precondition(state.selected.isEmpty, "Go must also validate results below min")

        let singleJSON = multipleJSON.replacingOccurrences(of: #""multiple":true"#, with: #""multiple":false"#)
        let single = try decoder.decode(MobilePresentationSelection.self, from: Data(singleJSON.utf8))
        state = MobilePresentationSelectionState(single)
        state.toggle(single.options[0])
        precondition(state.selected == ["first"])
        state.toggle(single.options[0])
        precondition(state.selected.isEmpty)

        let emptyJSON = multipleJSON.replacingOccurrences(of: #""selected":["second"]"#, with: #""selected":null"#)
        let empty = try decoder.decode(MobilePresentationSelection.self, from: Data(emptyJSON.utf8))
        precondition(MobilePresentationSelectionState(empty).selected.isEmpty)
        let lockedJSON = multipleJSON.replacingOccurrences(of: #""selected":["second"]"#, with: #""selected":["locked"]"#)
        let locked = try decoder.decode(MobilePresentationSelection.self, from: Data(lockedJSON.utf8))
        state = MobilePresentationSelectionState(locked)
        state.toggle(locked.options[2])
        precondition(state.selected == ["locked"], "preserve the Go-authored disabled state")

        let missingFlag = alertJSON.replacingOccurrences(of: #","commit":false"#, with: "")
        precondition((try? decoder.decode(MobilePresentation.self, from: Data(missingFlag.utf8))) == nil)
    }
}
`
