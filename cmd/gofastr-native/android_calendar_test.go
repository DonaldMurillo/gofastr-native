package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	renderandroid "github.com/DonaldMurillo/gofastr-native/render/android"
)

// TestAndroidCalendarTimeMatchesGo runs the renderer's RFC 3339 parser on
// times Go marshals, so the editor receives the instant Go validated.
func TestAndroidCalendarTimeMatchesGo(t *testing.T) {
	javac, err := exec.LookPath("javac")
	if err != nil {
		t.Skip("JDK unavailable")
	}
	java, err := exec.LookPath("java")
	if err != nil {
		t.Skip("Java runtime unavailable")
	}
	data, err := renderandroid.Sources.ReadFile("Services.java")
	if err != nil {
		t.Fatal(err)
	}
	_, tail, ok := strings.Cut(string(data), "    private static final java.util.regex.Pattern CALENDAR_TIME")
	body, _, found := strings.Cut(tail, "    private void calendar(JSONObject request)")
	if !ok || !found {
		t.Fatal("calendar time parser missing")
	}
	east, west := time.FixedZone("east", 5*3600+30*60), time.FixedZone("west", -8*3600)
	times := []time.Time{
		time.Date(2028, 2, 29, 9, 15, 30, 123456789, time.UTC),
		time.Date(2026, 3, 8, 0, 0, 0, 0, west),
		time.Date(2026, 12, 31, 23, 59, 59, 999000000, east),
		time.Date(1969, 12, 31, 0, 0, 0, 0, time.UTC),
		time.Date(1, 1, 1, 0, 0, 1, 0, time.UTC),
		time.Date(1600, 3, 1, 0, 0, 0, 0, west),
		time.Date(9999, 12, 31, 23, 0, 0, 0, time.UTC),
	}
	var cases, want strings.Builder
	for _, value := range times {
		text, err := value.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		year, month, day := value.Date()
		midnight := time.Date(year, month, day, 0, 0, 0, 0, time.UTC).UnixMilli()
		flag := 0
		if value.Hour() == 0 && value.Minute() == 0 && value.Second() == 0 && value.Nanosecond() == 0 {
			flag = 1
		}
		cases.WriteString(strconv.Quote(string(text)) + ",")
		want.WriteString(strconv.FormatInt(value.UnixMilli(), 10) + " " + strconv.FormatInt(midnight, 10) + " " + strconv.Itoa(flag) + "\n")
	}
	source := `public class CalendarTime {
    private static final java.util.regex.Pattern CALENDAR_TIME` + body + `
    public static void main(String[] args) {
        for (String value : new String[]{` + cases.String() + `}) {
            long[] parsed = calendarTime(value);
            System.out.print(parsed[0] + " " + parsed[1] + " " + parsed[2] + "\n");
        }
        for (String value : new String[]{"0001-01-01T00:00:00Z", "0000-12-31T00:00:00Z", "2026-02-29T00:00:00Z", "2026-01-01T24:00:00Z", "2026-01-01T00:00:60Z", "2026-01-01T00:00:00+24:00", "2026-01-01 00:00:00Z", "2026-01-01T00:00:00"}) {
            try { calendarTime(value); throw new AssertionError("accepted " + value); } catch (IllegalArgumentException expected) {}
        }
    }
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "CalendarTime.java")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(javac, "--release", "11", path).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, output)
	}
	output, err := exec.Command(java, "-cp", dir, "CalendarTime").CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, output)
	}
	if string(output) != want.String() {
		t.Fatalf("Java parsed\n%s\nGo expects\n%s", output, want.String())
	}
}
