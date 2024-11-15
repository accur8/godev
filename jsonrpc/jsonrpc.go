package jsonrpc

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"accur8.io/godev/log"

	"github.com/palantir/stacktrace"
)

type JsonRpcClient struct {
	rootUrl    Url
	httpClient *http.Client
	headers    map[string]string
}

type Url = url.URL

func UrlParse(urlStr string) *Url {
	u, e := url.Parse(urlStr)
	if e != nil {
		panic(e)
	}
	return u
}

func NewJsonRpcClient(rootUrl *Url, headers map[string]string) *JsonRpcClient {
	if headers == nil {
		headers = make(map[string]string)
	}

	return &JsonRpcClient{
		rootUrl:    *rootUrl,
		httpClient: &http.Client{},
		headers:    headers,
	}
}

func NewJsonRpcCaller[Req interface{}, Resp interface{}](path string, client *JsonRpcClient, typeConstructor func() *Resp) func(*Req) (*Resp, error) {

	url := AppendPath(&client.rootUrl, path)
	urlStr := url.String()

	return func(req *Req) (*Resp, error) {

		requestBodyBytes, err := json.Marshal(req)
		if err != nil {
			return nil, stacktrace.Propagate(err, "error marshalling request to json %v - %v", req, urlStr)
		}

		log.Debug("POST %v\n%v", urlStr, string(requestBodyBytes))

		httpRequest, err := http.NewRequest("POST", urlStr, bytes.NewReader(requestBodyBytes))
		if err != nil {
			return nil, stacktrace.Propagate(err, "error creating http request %v - %v", urlStr, requestBodyBytes)
		}

		for headerName, headerValue := range client.headers {
			httpRequest.Header.Set(headerName, headerValue)
		}

		httpRequest.Header.Set("Content-Type", "application/json")

		httpResponse, err := client.httpClient.Do(httpRequest)
		if err != nil {
			return nil, stacktrace.Propagate(err, "error making http request %v - %v", urlStr, httpRequest)
		}

		responseBody, err := io.ReadAll(httpResponse.Body)
		if err != nil {
			return nil, stacktrace.NewError("error reading http response body")
		}
		log.Debug("response %v\n%v", httpResponse.Status, string(responseBody))

		if httpResponse.StatusCode/100 != 2 {
			return nil, stacktrace.NewError("invalid http response %d", httpResponse.StatusCode)
		}

		responseObj := typeConstructor()
		// responseObjType := reflect.TypeOf(responseObj).Elem()
		// responseObjPtr := reflect.New(responseObjType)
		// responseObj2 := responseObjPtr.Elem().Interface().(Resp)
		err = json.Unmarshal(responseBody, responseObj)
		if err != nil {
			return nil, stacktrace.Propagate(err, "error Unmarshaling response body json - %s", string(responseBody))
		}

		return responseObj, nil

	}

}

func AppendPath(url0 *Url, path string) *Url {
	newUrl := *url0
	newPath := strings.TrimRight(url0.Path, "/") + "/" + strings.TrimLeft(path, "/")
	newUrl.Path = newPath
	return &newUrl
}
