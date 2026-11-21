package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (l pushLibrary) file() string { return l.group + "_" + l.name + "-" + l.version + "." + l.ext }

func (l pushLibrary) url() string {
	return string(l.repo) + strings.ReplaceAll(l.group, ".", "/") + "/" + l.name + "/" + l.version + "/" + l.name + "-" + l.version + "." + l.ext
}

// proprietary reports whether the artifact is under the Android Software
// Development Kit License rather than an open-source license.
func (l pushLibrary) proprietary() bool {
	return strings.HasPrefix(l.group, "com.google.firebase") || strings.HasPrefix(l.group, "com.google.android.gms") || strings.HasPrefix(l.group, "com.google.android.datatransport")
}

const pushLicenseNotice = `Firebase Cloud Messaging needs Firebase and Google Play services libraries
that are licensed under the Android Software Development Kit License
(https://developer.android.com/studio/terms) and Google APIs Terms of Service
(https://developers.google.com/terms); their use is also covered by the
Firebase terms (https://firebase.google.com/terms). The AndroidX, Kotlin,
okio and other supporting libraries are Apache-2.0. gofastr-native does not
redistribute any of them: this command downloads them for your own build.
`

func pushLibrariesCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("push-libraries", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "", "directory for the pinned Firebase Messaging libraries")
	accept := fs.Bool("accept-licenses", false, "accept the Android SDK License and Firebase terms for the downloaded libraries")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *out == "" || fs.NArg() > 0 {
		fmt.Fprintln(stderr, "usage: gofastr-native push-libraries --out DIR --accept-licenses")
		return 2
	}
	if !*accept {
		fmt.Fprint(stderr, pushLicenseNotice)
		fmt.Fprintln(stderr, "\nRe-run with --accept-licenses to download them.")
		return 2
	}
	if err := fetchPushLibraries(ctx, *out, stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "Verified %d libraries in %s. Build with --push-libraries %s --push-config google-services.json.\n", len(pushLibraries), *out, *out)
	return 0
}

func fetchPushLibraries(ctx context.Context, dir string, stdout io.Writer) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	for _, library := range pushLibraries {
		dst := filepath.Join(dir, library.file())
		if verifyPushLibrary(dst, library) == nil {
			continue
		}
		fmt.Fprintln(stdout, "download", library.url())
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, library.url(), nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, 64<<20))
		response.Body.Close()
		if err != nil {
			return err
		}
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("download %s: %s", library.url(), response.Status)
		}
		if sum := sha256.Sum256(body); hex.EncodeToString(sum[:]) != library.sha256 {
			return fmt.Errorf("download %s: SHA-256 does not match the pinned value", library.url())
		}
		if err := os.WriteFile(dst, body, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func verifyPushLibrary(file string, library pushLibrary) error {
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	if sum := sha256.Sum256(b); hex.EncodeToString(sum[:]) != library.sha256 {
		return fmt.Errorf("%s does not match the pinned SHA-256; run gofastr-native push-libraries again", file)
	}
	return nil
}

// firebaseValues are the resource strings the Firebase Gradle plugin would
// derive from google-services.json for one Android application id.
type firebaseValues struct {
	AppID, SenderID, APIKey, ProjectID, StorageBucket string
}

func readGoogleServices(file, appID string) (firebaseValues, error) {
	var config struct {
		ProjectInfo struct {
			ProjectNumber string `json:"project_number"`
			ProjectID     string `json:"project_id"`
			StorageBucket string `json:"storage_bucket"`
		} `json:"project_info"`
		Client []struct {
			ClientInfo struct {
				MobileSDKAppID    string `json:"mobilesdk_app_id"`
				AndroidClientInfo struct {
					PackageName string `json:"package_name"`
				} `json:"android_client_info"`
			} `json:"client_info"`
			APIKey []struct {
				CurrentKey string `json:"current_key"`
			} `json:"api_key"`
		} `json:"client"`
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return firebaseValues{}, err
	}
	if err := json.Unmarshal(b, &config); err != nil {
		return firebaseValues{}, fmt.Errorf("%s: %w", file, err)
	}
	for _, client := range config.Client {
		if client.ClientInfo.AndroidClientInfo.PackageName != appID {
			continue
		}
		values := firebaseValues{AppID: client.ClientInfo.MobileSDKAppID, SenderID: config.ProjectInfo.ProjectNumber, ProjectID: config.ProjectInfo.ProjectID, StorageBucket: config.ProjectInfo.StorageBucket}
		if len(client.APIKey) > 0 {
			values.APIKey = client.APIKey[0].CurrentKey
		}
		if values.AppID == "" || values.SenderID == "" || values.APIKey == "" || values.ProjectID == "" {
			return firebaseValues{}, fmt.Errorf("%s: the %s client lacks an app id, project number, API key or project id", file, appID)
		}
		return values, nil
	}
	return firebaseValues{}, fmt.Errorf("%s has no Android client for application id %s; register the app in the Firebase console", file, appID)
}

func (v firebaseValues) resource() []byte {
	var b bytes.Buffer
	b.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<resources>\n")
	for _, entry := range [][2]string{{"google_app_id", v.AppID}, {"gcm_defaultSenderId", v.SenderID}, {"google_api_key", v.APIKey}, {"project_id", v.ProjectID}, {"google_storage_bucket", v.StorageBucket}} {
		if entry[1] == "" {
			continue
		}
		b.WriteString("    <string name=\"" + entry[0] + "\" translatable=\"false\">")
		xml.EscapeText(&b, []byte(entry[1]))
		b.WriteString("</string>\n")
	}
	b.WriteString("</resources>\n")
	return b.Bytes()
}

// pushBuild is what a push-enabled build adds to the Java, dex and
// resource steps.
type pushBuild struct {
	jars, rSources, resourceArgs []string
	libraryManifests             [][]byte
	packages                     []string
}

// preparePushLibraries verifies and unpacks the pinned libraries and merges
// their manifests into a.PushPermissions and a.PushComponents.
func preparePushLibraries(r *runner, a *app, dir string) (pushBuild, error) {
	var build pushBuild
	err := r.do("verify and unpack the Firebase Messaging libraries", func() error {
		if _, err := readGoogleServices(a.PushConfig, a.ID); err != nil {
			return err
		}
		root := filepath.Join(dir, "push")
		if err := os.RemoveAll(root); err != nil {
			return err
		}
		for _, library := range pushLibraries {
			file := filepath.Join(a.PushLibraries, library.file())
			if err := verifyPushLibrary(file, library); err != nil {
				return err
			}
			if library.ext == "jar" {
				build.jars = append(build.jars, file)
				continue
			}
			dst := filepath.Join(root, library.group+"_"+library.name)
			manifest, jars, err := unpackLibraryAAR(file, dst)
			if err != nil {
				return fmt.Errorf("%s: %w", file, err)
			}
			build.jars = append(build.jars, jars...)
			build.libraryManifests = append(build.libraryManifests, manifest)
			if isDir(filepath.Join(dst, "res")) {
				pkg, err := manifestPackage(manifest)
				if err != nil {
					return fmt.Errorf("%s: %w", file, err)
				}
				build.packages = append(build.packages, pkg)
			}
		}
		permissions, components, err := mergeLibraryManifests(build.libraryManifests, a.ID, androidHostPermissions(*a))
		a.PushPermissions, a.PushComponents = permissions, components
		return err
	})
	return build, err
}

// unpackLibraryAAR extracts classes.jar, libs/*.jar, res/ and the manifest.
func unpackLibraryAAR(aar, dst string) ([]byte, []string, error) {
	zr, err := zip.OpenReader(aar)
	if err != nil {
		return nil, nil, err
	}
	defer zr.Close()
	root, err := mkRoot(dst)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	var manifest []byte
	var jars []string
	for _, f := range zr.File {
		name := path.Clean(f.Name)
		if strings.HasSuffix(f.Name, "/") {
			continue
		}
		switch {
		case name == "AndroidManifest.xml":
			rc, err := f.Open()
			if err != nil {
				return nil, nil, err
			}
			manifest, err = io.ReadAll(io.LimitReader(rc, 1<<20))
			rc.Close()
			if err != nil {
				return nil, nil, err
			}
		case name == "classes.jar" || strings.HasPrefix(name, "libs/") && strings.HasSuffix(name, ".jar"):
			jars = append(jars, filepath.Join(dst, filepath.FromSlash(name)))
		case strings.HasPrefix(name, "res/") && strings.Count(name, "/") == 2:
		default:
			continue
		}
		if err := copyZipEntry(root, f, name); err != nil {
			return nil, nil, err
		}
	}
	if manifest == nil {
		return nil, nil, errors.New("no AndroidManifest.xml")
	}
	return manifest, jars, nil
}

func manifestPackage(manifest []byte) (string, error) {
	var m struct {
		Package string `xml:"package,attr"`
	}
	if err := xml.Unmarshal(manifest, &m); err != nil || m.Package == "" {
		return "", errors.New("manifest has no package")
	}
	return m.Package, nil
}

// androidHostPermissions are the permissions the generated manifest always
// declares, so merged library permissions do not repeat them.
func androidHostPermissions(a app) map[string]bool {
	seen := map[string]bool{"android.permission.INTERNET": true, "android.permission.POST_NOTIFICATIONS": true, "android.permission.RECEIVE_BOOT_COMPLETED": true}
	for _, permission := range a.AndroidExtensionPermissions() {
		seen[permission] = true
	}
	if a.MicrophoneUsage != "" {
		seen["android.permission.RECORD_AUDIO"] = true
	}
	if a.SecureStorage && a.BiometricUsage != "" {
		seen["android.permission.USE_BIOMETRIC"] = true
	}
	return seen
}

const androidNS = "http://schemas.android.com/apk/res/android"

type xmlNode struct {
	name     string
	attrs    []xml.Attr
	children []*xmlNode
}

func (n *xmlNode) attr(local string) string {
	for _, a := range n.attrs {
		if a.Name.Space == androidNS && a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

func parseXMLNode(b []byte) (*xmlNode, error) {
	decoder := xml.NewDecoder(bytes.NewReader(b))
	var stack []*xmlNode
	var root *xmlNode
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			node := &xmlNode{name: t.Name.Local}
			for _, a := range t.Attr {
				if a.Name.Space == androidNS {
					node.attrs = append(node.attrs, a)
				}
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, node)
			} else {
				root = node
			}
			stack = append(stack, node)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
	if root == nil || root.name != "manifest" {
		return nil, errors.New("not an Android manifest")
	}
	return root, nil
}

func (n *xmlNode) write(b *strings.Builder, indent, appID string) {
	b.WriteString(indent + "<" + n.name)
	for _, a := range n.attrs {
		b.WriteString(" android:" + a.Name.Local + "=\"")
		xml.EscapeText(stringWriter{b}, []byte(strings.ReplaceAll(a.Value, "${applicationId}", appID)))
		b.WriteString("\"")
	}
	if len(n.children) == 0 {
		b.WriteString(" />\n")
		return
	}
	b.WriteString(">\n")
	for _, child := range n.children {
		child.write(b, indent+"    ", appID)
	}
	b.WriteString(indent + "</" + n.name + ">\n")
}

type stringWriter struct{ b *strings.Builder }

func (w stringWriter) Write(p []byte) (int, error) { return w.b.Write(p) }

// mergeLibraryManifests is the subset of the Gradle manifest merger the
// Firebase libraries need: permissions and components are unioned by name,
// and the children of a component declared by several libraries (Firebase's
// ComponentDiscoveryService registrars) are combined.
func mergeLibraryManifests(manifests [][]byte, appID string, skip map[string]bool) (string, string, error) {
	var permissions, components []*xmlNode
	seen := map[string]*xmlNode{}
	for _, manifest := range manifests {
		root, err := parseXMLNode(manifest)
		if err != nil {
			return "", "", err
		}
		for _, child := range root.children {
			switch child.name {
			case "uses-permission", "permission":
				name := strings.ReplaceAll(child.attr("name"), "${applicationId}", appID)
				if name == "" || skip[child.name+":"+name] || child.name == "uses-permission" && skip[name] {
					continue
				}
				skip[child.name+":"+name] = true
				permissions = append(permissions, child)
			case "application":
				for _, component := range child.children {
					name := component.attr("name")
					if name == "" {
						continue
					}
					key := component.name + ":" + name
					if existing := seen[key]; existing != nil {
						have := map[string]bool{}
						for _, c := range existing.children {
							have[c.name+":"+c.attr("name")] = true
						}
						for _, c := range component.children {
							if !have[c.name+":"+c.attr("name")] {
								existing.children = append(existing.children, c)
							}
						}
						continue
					}
					seen[key] = component
					components = append(components, component)
				}
			}
		}
	}
	sort.SliceStable(components, func(i, j int) bool { return components[i].attr("name") < components[j].attr("name") })
	var p, c strings.Builder
	for _, node := range permissions {
		node.write(&p, "    ", appID)
	}
	for _, node := range components {
		node.write(&c, "        ", appID)
	}
	return p.String(), c.String(), nil
}

// preparePushResources compiles the Firebase values and every library's
// resources, then links them once to generate the R classes the library
// bytecode reads. The emitted ids keep the final link identical.
func preparePushResources(r *runner, sdk androidSDK, a app, dir, manifest string, build *pushBuild, featureArgs []string) error {
	root := filepath.Join(dir, "push")
	values := filepath.Join(root, "values", "res", "values", "gofastr_push.xml")
	if err := r.do("generate Firebase configuration resources", func() error {
		config, err := readGoogleServices(a.PushConfig, a.ID)
		if err != nil {
			return err
		}
		return writeSource(values, config.resource())
	}); err != nil {
		return err
	}
	compiled := filepath.Join(root, "values.zip")
	if err := r.run("compile Firebase configuration resources", sdk.tool("aapt2"), "compile", "-o", compiled, values); err != nil {
		return err
	}
	args := []string{"--auto-add-overlay"}
	for _, library := range pushLibraries {
		res := filepath.Join(root, library.group+"_"+library.name, "res")
		if library.ext != "aar" || (!r.dryRun && !isDir(res)) {
			continue
		}
		zip := filepath.Join(root, library.group+"_"+library.name+".zip")
		if err := r.run("compile "+library.name+" resources", sdk.tool("aapt2"), "compile", "--dir", res, "-o", zip); err != nil {
			return err
		}
		args = append(args, "-R", zip)
	}
	args = append(args, "-R", compiled)
	ids := filepath.Join(root, "ids.txt")
	gen := filepath.Join(root, "gen")
	link := append(aapt2LinkArgs(sdk, a, manifest, filepath.Join(root, "r.apk"), append(append([]string(nil), featureArgs...), args...), false),
		"--java", gen, "--emit-ids", ids)
	if len(build.packages) > 0 {
		link = append(link, "--extra-packages", strings.Join(build.packages, ":"))
	}
	if err := r.do("clear old generated R classes", func() error { return os.RemoveAll(gen) }); err != nil {
		return err
	}
	if err := r.run("generate R classes for the Firebase libraries", sdk.tool("aapt2"), link...); err != nil {
		return err
	}
	build.rSources = []string{filepath.Join(gen, "<R.java>")}
	if !r.dryRun {
		sources, err := listFiles(gen, ".java")
		if err != nil {
			return err
		}
		build.rSources = sources
	}
	build.resourceArgs = append(args, "--stable-ids", ids)
	return nil
}
