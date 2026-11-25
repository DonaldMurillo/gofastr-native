package main

import (
	"archive/zip"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const arcoreVersion = "1.57.0"
const arcoreSHA256 = "df53bb8ea8e3a9f32e256b91909959d60b2e4c60d8fcc56b905b4c3e926f5c72"

func prepareARCore(r *runner, a *app, dir, jniDir string) ([]string, error) {
	root := filepath.Join(dir, "arcore")
	jars := []string{filepath.Join(root, "classes.jar")}
	err := r.do("verify and unpack ARCore "+arcoreVersion, func() error {
		b, err := os.ReadFile(a.ARCoreAAR)
		if err != nil {
			return err
		}
		if fmt.Sprintf("%x", sha256.Sum256(b)) != arcoreSHA256 {
			return fmt.Errorf("--arcore-aar must be the Google Maven ARCore %s AAR (SHA-256 %s)", arcoreVersion, arcoreSHA256)
		}
		if err := os.RemoveAll(root); err != nil {
			return err
		}
		manifest, extracted, err := unpackLibraryAAR(a.ARCoreAAR, root)
		if err != nil {
			return err
		}
		jars = extracted
		_, a.ARCoreComponents, err = mergeLibraryManifests([][]byte{manifest}, a.ID, map[string]bool{})
		if err != nil {
			return err
		}
		return addARCoreNativeLibraries(a.ARCoreAAR, jniDir, a.ABIs)
	})
	return jars, err
}

func addARCoreNativeLibraries(aar, jniDir string, abis []string) error {
	zr, err := zip.OpenReader(aar)
	if err != nil {
		return err
	}
	defer zr.Close()
	root, err := os.OpenRoot(jniDir)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, abi := range abis {
		for _, lib := range []string{"libarcore_sdk_c.so", "libarcore_sdk_jni.so"} {
			name := abi + "/" + lib
			var entry *zip.File
			for _, file := range zr.File {
				if file.Name == "jni/"+name {
					entry = file
					break
				}
			}
			if entry == nil {
				return fmt.Errorf("ARCore %s lacks %s", arcoreVersion, name)
			}
			if err := copyZipEntry(root, entry, name); err != nil {
				return err
			}
		}
	}
	return nil
}

func compileARCoreResources(r *runner, sdk androidSDK, dir string) ([]string, error) {
	root := filepath.Join(dir, "arcore")
	compiled := filepath.Join(root, "res.zip")
	err := r.run("compile ARCore resources", sdk.tool("aapt2"), "compile", "--dir", filepath.Join(root, "res"), "-o", compiled)
	return []string{"-R", compiled}, err
}

func linkAndroidLibraryResources(r *runner, sdk androidSDK, a app, dir, manifest string, resources, packages []string) ([]string, string, error) {
	root := filepath.Join(dir, "library-resources")
	ids, gen := filepath.Join(root, "ids.txt"), filepath.Join(root, "gen")
	link := append(aapt2LinkArgs(sdk, a, manifest, filepath.Join(root, "r.apk"), resources, false),
		"--java", gen, "--emit-ids", ids)
	if len(packages) > 0 {
		link = append(link, "--extra-packages", strings.Join(packages, ":"))
	}
	if err := r.do("clear old generated R classes", func() error {
		if err := os.RemoveAll(root); err != nil {
			return err
		}
		return os.MkdirAll(root, 0o700)
	}); err != nil {
		return nil, "", err
	}
	if err := r.run("generate R classes for Android libraries", sdk.tool("aapt2"), link...); err != nil {
		return nil, "", err
	}
	if r.dryRun {
		return []string{filepath.Join(gen, "<R.java>")}, ids, nil
	}
	sources, err := listFiles(gen, ".java")
	return sources, ids, err
}
