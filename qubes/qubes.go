package qubes

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	. "github.com/palantir/stacktrace"
)

type QubesClient struct {
	rootURL string // RootURL of the Qubes instance, should be domain and nothing else i.e. https://myqubes.accur8.io

	// Retries is the number of times the RemoteApi will retry an API request
	// if it fails with a timeout or temporary kind of error, or an HTTP
	// status of 408 or 5XX.
	// MaxRetries int

	userAgent string

	// IncrementalBackoff is the duration used as the unit of linear backoff.
	// IncrementalBackoff time.Duration
	authToken  string
	httpClient *http.Client
}

type qubesQueryImpl[A interface{}] struct {
	client *QubesClient
}

type QubesQuery[A interface{}] interface {
	Update(request *CrudRowRequest[A], row *A) (*CrudRowResponse, error)
	Insert(request *CrudRowRequest[A], row *A) (*CrudRowResponse, error)
	// Upsert(a A, existenceCheckBiql string) (*A, error)
	Delete(request *CrudRowRequest[A], row *A) error
	FetchOne(biql string) (*A, error)
	Fetch(biql string) (*A, error)
	Select(biql string) ([]*A, error)
}

type qubesQueryRequest struct {
	query      string
	dataFormat string
	appSpace   string
}

type qubesQueryResponse[A interface{}] struct {
	data []*A
}

type CrudRowRequest[A interface{}] struct {
	Cube   string
	Fields json.RawMessage
	// parameters: JsArray = JsArray(Nil),
	Where    string
	AppSpace string
}

type CrudRowResponse struct {
	success             bool
	validationFailures  json.RawMessage
	errorMessage        string
	serverStackTrace    string
	numberOfRowsUpdated int
	keys                json.RawMessage
}

func runRpc[Req interface{}, Resp interface{}](path string, qc *QubesClient, req *Req, resp *Resp) error {
	requestBody, err := json.Marshal(req)
	if err != nil {
		return Propagate(err, "unable to Marshal", requestBody)
	}

	url := qc.rootURL + path

	response, err := qc.httpClient.Post(url, "application/json", bytes.NewReader(requestBody))
	if err != nil {
		return Propagate(err, "error processing rpc request to", url)
	}

	if response.StatusCode%100 != 2 {
		return NewError("inavlid response code %d", response.StatusCode)
	}

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return Propagate(err, "error reading response body from", url)
	}

	err = json.Unmarshal(responseBody, resp)
	if err != nil {
		return Propagate(err, "error reading marshalling json", responseBody)
	}

	return nil

}

func NewQubesClient(rootURL string, authToken string) *QubesClient {
	return &QubesClient{
		rootURL: rootURL,
		// MaxRetries: 5,
		authToken: authToken,
		httpClient: &http.Client{
			// Transport: http.NewDefaultTransportWithResumption(),
			Timeout: time.Minute,
		},
		// userAgent: fmt.Sprintf("%s/%s", Name, Version),
	}

}

func Query[A any](qc *QubesClient) QubesQuery[A] {
	return &qubesQueryImpl[A]{
		client: qc,
	}
}

func (qq *qubesQueryImpl[A]) runCrudRequest(path string, request *CrudRowRequest[A], row *A) (*CrudRowResponse, error) {

	var err error

	request.Fields, err = json.Marshal(row)
	if err != nil {
		return nil, Propagate(err, "unable to marshal instance to json", row)
	}

	responseObj := &CrudRowResponse{}

	err = runRpc(path, qq.client, request, responseObj)
	if err != nil {
		return nil, Propagate(err, "request failed", path, row)
	}

	if !responseObj.success {
		return nil, Propagate(err, "crud response.success = false", responseObj)
	}

	return responseObj, nil

}

func (qq *qubesQueryImpl[A]) Update(request *CrudRowRequest[A], row *A) (*CrudRowResponse, error) {
	response, err := qq.runCrudRequest("/api/ipdate", request, row)
	if err != nil {
		return nil, Propagate(err, "/api/update runCrudRequest", row)
	}
	return response, nil
}

func (qq *qubesQueryImpl[A]) Insert(request *CrudRowRequest[A], row *A) (*CrudRowResponse, error) {
	response, err := qq.runCrudRequest("/api/insert", request, row)
	if err != nil {
		return nil, Propagate(err, "/api/insert runCrudRequest", row)
	}
	return response, nil
}

func (qq *qubesQueryImpl[A]) Delete(request *CrudRowRequest[A], row *A) error {
	_, err := qq.runCrudRequest("/api/delete", request, row)
	if err != nil {
		return Propagate(err, "/api/delete runCrudRequest", row)
	}
	return nil
}

func (qq *qubesQueryImpl[A]) FetchOne(biql string) (*A, error) {
	rows, err := qq.Select(biql)
	if err != nil {
		return nil, err
	}
	switch len(rows) {
	case 1:
		return rows[1], nil
	default:
		return nil, NewError("returned %d rows and expected 1 row", len(rows))
	}
}

func (qq *qubesQueryImpl[A]) Fetch(biql string) (*A, error) {
	rows, err := qq.Select(biql)
	if err != nil {
		return nil, err
	}
	switch len(rows) {
	case 0:
		return nil, nil
	case 1:
		return rows[1], nil
	default:
		return nil, NewError("returned %d rows and expected 0 or 1 row", len(rows))
	}
}

func (qq *qubesQueryImpl[A]) Select(biql string) ([]*A, error) {

	request := &qubesQueryRequest{
		query:      biql,
		dataFormat: "verbose",
	}

	arr := make([]*A, 8)

	err := runRpc("/api/query", qq.client, request, &arr)
	if err != nil {
		return nil, err
	}

	return arr, nil
}
