package httpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"accur8.io/godev/a8"
	"accur8.io/godev/log"
	"github.com/gorilla/mux"
)

type HttpError struct {
	Msg        string
	StatusCode int
}

type HasHttpStatusCode interface {
	GetStatusCode() int
}

func (he *HttpError) GetStatusCode() int {
	return he.StatusCode
}

func NewError(msg string, statusCode int) error {
	return &HttpError{
		Msg:        msg,
		StatusCode: statusCode,
	}
}

func (he HttpError) Error() string {
	return fmt.Sprintf("%d - %s", he.StatusCode, he.Msg)
}

func GetHttpStatusCode(err error) int {
	for _, sc := range UnwrapHasHttpStatusCode(err) {
		return sc.GetStatusCode()
	}
	return 500
}

func UnwrapHasHttpStatusCode(err error) []HasHttpStatusCode {
	arr := []HasHttpStatusCode{}
	for _, err := range a8.UnwrapErrorChain(err) {
		he, ok := err.(HasHttpStatusCode)
		if ok {
			arr = append(arr, he)
		}
	}
	return arr
}

type HttpResponse interface {
	Respond(w http.ResponseWriter)
}

func ErrResponse(err error, code int) HttpResponse {
	return &ErrorResponse{
		code:    code,
		message: err.Error(),
	}
}

func ErrStrResponse(errorMessage string, code int) HttpResponse {
	return &ErrorResponse{
		code:    code,
		message: errorMessage,
	}
}

type ErrorResponse struct {
	code    int
	message string
}

func (ep *ErrorResponse) Respond(w http.ResponseWriter) {
	http.Error(w, ep.message, ep.code)
}

func JsonRpc[Req any, Resp any, Services any](
	services Services,
	processor func(Services, *Req) (*Resp, HttpResponse),
) func(w http.ResponseWriter, r *http.Request) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		log.Debug("starting %s", r.URL)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		var request Req
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		err := decoder.Decode(&request)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp, errorResp := processor(services, &request)
		// log.Printf("response %v", resp)
		if resp != nil {
			jsonResponseBody, err := json.Marshal(resp)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			} else {
				log.Debug("responding with - %s", jsonResponseBody)
				w.Header().Set("Content-Type", "application/json")
				w.Write(jsonResponseBody)
			}
		} else if errorResp != nil {
			errorResp.Respond(w)
		} else {
			// empty ok response
			w.WriteHeader(200)
		}
	}
	return handler
}

func JsonRpcE[Req any, Resp any, Services any](
	services Services,
	processor func(Services, *Req) (*Resp, error),
) func(w http.ResponseWriter, r *http.Request) {
	delegate := func(services Services, req *Req) (*Resp, HttpResponse) {
		resp, err := processor(services, req)
		if err != nil {
			statusCode := GetHttpStatusCode(err)
			return resp, ErrResponse(err, statusCode)
		} else {
			return resp, nil
		}
	}
	return JsonRpc(services, delegate)
}

func RouteDescription(router *mux.Router) string {
	routeLines := make([]string, 10)
	appendLine := func(a ...any) {
		routeLines = append(routeLines, "    ")
		routeLines = append(routeLines, fmt.Sprintln(a...))
		// routeLines = append(routeLines, "\n")
	}
	appendLine("")
	appendLine("")
	router.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
		pathTemplate, err := route.GetPathTemplate()
		if err == nil {
			appendLine("ROUTE:", pathTemplate)
		}
		pathRegexp, err := route.GetPathRegexp()
		if err == nil {
			appendLine("Path regexp:", pathRegexp)
		}
		queriesTemplates, err := route.GetQueriesTemplates()
		if err == nil {
			appendLine("Queries templates:", strings.Join(queriesTemplates, ","))
		}
		queriesRegexps, err := route.GetQueriesRegexp()
		if err == nil {
			appendLine("Queries regexps:", strings.Join(queriesRegexps, ","))
		}
		methods, err := route.GetMethods()
		if err == nil {
			appendLine("Methods:", strings.Join(methods, ","))
		}
		appendLine("")
		return nil
	})
	return fmt.Sprintf("%s", routeLines)
}

func SetCorsHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
}
