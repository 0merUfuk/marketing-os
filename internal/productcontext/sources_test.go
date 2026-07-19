package productcontext

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/omerufuk/marketing-os/internal/domain"
)

func TestSourceRedirectIsRevalidatedAgainstURLPolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, "http://192.168.1.10/private", http.StatusFound)
	}))
	defer server.Close()
	service := Service{}
	_, warnings, err := service.collectSources(context.Background(), domain.Product{ID: "alpha", Name: "Alpha", Website: server.URL, ProductType: "saas", PrimaryConversionAction: "trial", DefaultLanguage: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "unsafe source redirect") {
		t.Fatalf("warnings=%#v", warnings)
	}
}
