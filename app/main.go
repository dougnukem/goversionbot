package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"golang.org/x/mod/semver"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/darrenmcc/dizmo"
)

const collection = "goversion"

var slackURL string

func main() {
	slackURL = mustEnv("SLACK_URL")
	http.Handle("/", dizmo.LogMiddleware(http.HandlerFunc(do)))
	http.ListenAndServe(":"+mustEnv("PORT"), nil)
}

const dlURL = "https://go.dev/dl/"

func do(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	resp, err := http.Get(dlURL)
	if err != nil {
		dizmo.Errorf(ctx, "unable to fetch %s: %s", dlURL, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var version string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "downloadBox") && strings.Contains(line, "darwin-amd64.pkg") {
			// strip line down to goX.YY.ZZ version
			version = strings.TrimPrefix(
				strings.TrimSuffix(line, `.darwin-amd64.pkg">`),
				`<a class="download downloadBox" href="/dl/`)
			dizmo.Infof(ctx, "latest version: %s", version)
			break
		}
	}

	if version == "" {
		dizmo.Errorf(ctx, "no version found in go.dev/dl page")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	fs, err := firestore.NewClient(ctx, dizmo.GoogleProjectID())
	if err != nil {
		dizmo.Errorf(ctx, "unable to init firestore client: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	err = fs.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		// see if this version exists in firestore
		doc := fs.Doc(key(version))
		_, err := doc.Get(ctx)
		stat, _ := status.FromError(err)
		switch {
		case stat == nil:
			// version already exists
			dizmo.Infof(ctx, "current status matches latest: %s", version)
		case stat.Code() == codes.NotFound:
			// new version!
			dizmo.Infof(ctx, "new version!")
			b, err := json.Marshal(map[string]string{
				"text": getGoVersionMessage(version),
			})
			if err != nil {
				return fmt.Errorf("unable to marshal slack request: %s", err)
			}

			// post message to slack
			slackResp, err := http.Post(slackURL, "application/json", bytes.NewReader(b))
			if err != nil {
				return fmt.Errorf("unable to post slack message: %s", err)
			}
			defer slackResp.Body.Close()

			if slackResp.StatusCode != http.StatusOK {
				b, _ = httputil.DumpResponse(slackResp, true)
				return fmt.Errorf("non-200 slack response: %s", b)
			}

			// delete old version records
			docs, err := fs.Collection(collection).DocumentRefs(ctx).GetAll()
			if err != nil {
				return fmt.Errorf("unable to get old version from firestore: %s", err)
			}
			for _, docref := range docs {
				_, err = docref.Delete(ctx)
				if err != nil {
					return fmt.Errorf("unable to delete old version %q from firestore: %s", docref.Path, err)
				}
			}

			// write new version record
			_, err = fs.Doc(key(version)).Create(ctx, map[string]string{
				"version": version,
				"date":    time.Now().Format("2006-01-02"),
			})
			if err != nil {
				return fmt.Errorf("unable to write new version to firestore: %s", err)
			}
		default:
			return fmt.Errorf("unable to fetch firestore document: %s", err)
		}
		return nil
	})
	if err != nil {
		dizmo.Errorf(ctx, err.Error())
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func getGoVersionMessage(version string) string {
	// Show the github milestone issues in this release e.g.
	// for minor release https://github.com/golang/go/issues?q=milestone%3AGo1.19.4+label%3ACherryPickApproved+
	// for major releasehttps://github.com/golang/go/issues?q=milestone%3AGo1.19+
	gitMilestoneURL := fmt.Sprintf("https://github.com/golang/go/issues?q=milestone%%3AGo%s+label%%3ACherryPickApproved+", version)

	goDocReleaseNotesURL := "https://go.dev/doc/devel/release"
	dlURLVersion := version
	// valid semantic version go1.19.14 = v1.19.4
	semVersion := "v" + strings.TrimPrefix(version, "go")
	if semver.IsValid(semVersion) {
		majorMinorVersion := semver.MajorMinor(semVersion)
		if isMinorRelease(semVersion) {
			// v1.19.4 URL is https://go.dev/doc/devel/release#go1.19.minor
			goDocReleaseNotesURL = fmt.Sprintf("https://go.dev/doc/devel/release#go%s.minor", majorMinorVersion)
			// v1.19.4 git milestone https://github.com/golang/go/issues?q=milestone%3AGo1.19.4+label%3ACherryPickApproved+
			gitMilestoneURL = fmt.Sprintf("https://github.com/golang/go/issues?q=milestone%%3AGo%s+label%%3ACherryPickApproved+", version)
		} else {

			// for major versions the DL link for v1.19.0 is https://dl.google.com/go/go1.19.darwin-amd64.pkg
			dlURLVersion = "go" + majorMinorVersion
			// release notes v1.19.0 URL is https://go.dev/doc/devel/release#go1.19
			goDocReleaseNotesURL = fmt.Sprintf("https://go.dev/doc/devel/release#go%s", majorMinorVersion)
			// git milestone v1.19.0 URL is https://github.com/golang/go/issues?q=milestone%3AGo1.19
			gitMilestoneURL = fmt.Sprintf("https://github.com/golang/go/issues?q=milestone%%3AGo%s", majorMinorVersion)
		}
	}

	return fmt.Sprintf("A new Go version [%s] is available, download for MacOS here: %s%s.darwin-amd64.pkg <Release Notes|%s> <Github Milestone|%s>", version, dlURL, dlURLVersion, goDocReleaseNotesURL, gitMilestoneURL)
}

func key(version string) string {
	return collection + "/" + version
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		panic(k + " not found in environment")
	}
	return v
}

func isMinorRelease(version string) bool {
	if !semver.IsValid(version) {
		return false
	}

	// compare 1.19.1 to the canonical 1.19.0 major/minor release if this is > then its a minor release <= 1 it's the same
	majorReleaseVersion := semver.MajorMinor(version) + ".0"
	return semver.Compare(version, majorReleaseVersion) >= 1
}
