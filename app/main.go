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
	"github.com/darrenmcc/dizmo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func main() {
	http.Handle("/", dizmo.LogMiddleware(http.HandlerFunc(do)))
	http.ListenAndServe(":"+os.Getenv("PORT"), nil)
}

func do(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	fs, err := firestore.NewClient(ctx, dizmo.GoogleProjectID())
	if err != nil {
		dizmo.Errorf(ctx, "unable to init firestore client: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	const dlURL = "https://go.dev/dl/"
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

	err = fs.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		const collection = "goversion"
		// see if this version exists in firestore
		doc := fs.Doc(collection + "/" + version)
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
				"text": fmt.Sprintf("A new Go version [%s] is available, download for MacOS here: %s%s.darwin-amd64.pkg", version, dlURL, version),
			})
			slackResp, err := http.Post(os.Getenv("SLACK_URL"), "application/json", bytes.NewReader(b))
			if err != nil {
				return fmt.Errorf("unable to post slack message: %s", err)
			}
			defer slackResp.Body.Close()

			if slackResp.StatusCode != http.StatusOK {
				b, _ = httputil.DumpResponse(slackResp, true)
				return fmt.Errorf("non-200 slack response: %s", b)
			}
			docs, err := fs.Collection(collection).DocumentRefs(ctx).GetAll()
			if err != nil {
				return fmt.Errorf("unable to delete old version %q from firestore: %s", doc.Path, err)
			}
			for _, docref := range docs {
				_, err = docref.Delete(ctx)
				if err != nil {
					return fmt.Errorf("unable to delete old version %q from firestore: %s", docref.Path, err)
				}
			}
			_, err = fs.Doc("goversion/"+version).Create(ctx, map[string]string{
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
