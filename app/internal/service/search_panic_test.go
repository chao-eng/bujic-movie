package service

import (
	"context"
	"errors"
	"testing"

	"github.com/bujic-movie/bujic-movie/internal/service/connector"
)

// panicConnector is a definition that blows up mid-search, the way a broken
// filter did before validate grew its list form.
type panicConnector struct{}

func (panicConnector) Name() string { return "boom" }
func (panicConnector) Test(context.Context) (*connector.Capabilities, error) {
	panic("template exploded")
}
func (panicConnector) Search(context.Context, connector.Query) ([]connector.Release, error) {
	panic("runtime error: index out of range [1] with length 1")
}
func (panicConnector) ResolveDownload(context.Context, connector.Release) (*connector.DownloadRef, error) {
	return nil, nil
}

// A panic inside one site must become a per-site failure, otherwise a single
// bad definition takes down the whole fan-out and the API request with it.
func TestSearchSurvivesSitePanic(t *testing.T) {
	releases, err := safeSiteSearch(panicConnector{}, context.Background(), connector.Query{Keyword: "x"}, "boom")
	if err == nil {
		t.Fatal("a panicking site must report a failure")
	}
	if releases != nil {
		t.Errorf("releases = %v, want nil", releases)
	}
	var cErr *connector.Error
	if !errors.As(err, &cErr) {
		t.Fatalf("error must be a connector error, got %T", err)
	}
	if cErr.Kind != connector.KindParse {
		t.Errorf("kind = %q, want %q", cErr.Kind, connector.KindParse)
	}
}

// The same guard exists on the connectivity test path.
func TestIndexerTestSurvivesPanic(t *testing.T) {
	caps, err := safeConnectorTest(panicConnector{}, context.Background())
	if err == nil {
		t.Fatal("a panicking site must report a failure")
	}
	if caps != nil {
		t.Errorf("caps = %v, want nil", caps)
	}
}
