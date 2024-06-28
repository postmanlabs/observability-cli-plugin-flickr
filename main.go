// Flickr's Akita Plugin
//
// Since Flickr's REST API does not yet follow every modern REST convention,
// this plugin serves to bridge the gap between old and new by translating
// captured http requests into a format that is more readily consumed by
// Akita.
//
// 1) Flickr's API `method` query string param gets mapped into a pseudo path.
//
// 2) Flickr's API Key `api_key` query string param gets mapped into a pseudo
//    Authorization Basic.
//
// 3) Flickr's response element `stat` will override the response's HTTP
//    Status Code when failure is indicated, mapping to a 400 response.
//
// 4) Flickr's NSID account IDs will be detected as an Akita custom format.
//
package plugin_flickr

import (
	pb "github.com/akitasoftware/akita-ir/go/api_spec"
	"github.com/akitasoftware/akita-libs/spec_util"
	"github.com/akitasoftware/akita-libs/spec_util/ir_hash"
	"github.com/postmanlabs/postman-insights-agent/plugin"
	"github.com/postmanlabs/postman-insights-agent/printer"

	"errors"
	"fmt"

	"regexp"
	"strings"
)

type FlickrAkitaPlugin struct{}

func (fp FlickrAkitaPlugin) Name() string {
	printer.Debugf("Flickr plugin.Name called, starting up!\n")
	return "FlickrAkitaPlugin"
}

func DataToString(data *pb.Data) *pb.String {
	if prim := data.GetPrimitive(); prim != nil {
		if val := prim.GetStringValue(); val != nil {
			return val
		}
	}

	return nil
}

func PopQueryStringParamByName(method *pb.Method, name string) *pb.String {
	var query *pb.HTTPQuery
	var qsp *pb.String

	// After we find our param, delete. There could be multiple with
	// the same name and we need to clean them all up.
	keysToDelete := make([]string, 0, 1)

	for k, data := range method.GetArgs() {
		// Query String Params are structured as independent key/value pairs
		if query = spec_util.HTTPQueryFromData(data); query == nil {
			continue
		}

		if query.GetKey() != name {
			continue
		}

		keysToDelete = append(keysToDelete, k)

		// Gymnastics to only use the first value we see.
		if tmpQsp := DataToString(data); tmpQsp != nil && qsp == nil {
			qsp = tmpQsp
		}
	}

	for _, k := range keysToDelete {
		delete(method.Args, k)
	}

	return qsp
}

func PopMultipartElementByName(s *pb.Struct, name string) *pb.String {
	if s == nil {
		return nil
	}

	data, present := s.Fields[name]
	if !present {
		return nil
	}

	if qsp := DataToString(data); qsp != nil {
		delete(s.Fields, name)
		return qsp
	}

	return nil
}

func PopBodyElementByName(method *pb.Method, name string) *pb.String {
	var m *pb.DataMeta_Http
	var body *pb.Struct
	var ok bool

	for _, data := range method.GetArgs() {
		// Ignore if not DataMeta_Http
		if m, ok = data.GetMeta().GetMeta().(*pb.DataMeta_Http); !ok {
			continue
		}

		// Check if a multipart form body, check each part if so.
		// There should not be additional bodies if so, so we can just
		// terminate the search early.
		if mp := m.Http.GetMultipart(); mp != nil {
			return PopMultipartElementByName(data.GetStruct(), name)
		}

		// Ignore if not HTTPMeta_Body
		if _, ok = m.Http.Location.(*pb.HTTPMeta_Body); !ok {
			continue
		}

		if body = data.GetStruct(); body == nil {
			continue
		}

		if _, ok := body.Fields[name]; !ok {
			continue
		}

		if qsp := DataToString(body.Fields[name]); qsp != nil {
			delete(body.Fields, name)

			return qsp
		}
	}

	return nil
}

var nsid_re = regexp.MustCompile("^[0-9]+@N[0-9]+$")

func detectNSIDsData(data *pb.Data) {
	// Recurse through whatever `data` is, updating its format if it's a primitive
	// and contains a Flickr NSID value.

	// Note: Whenever we add format detection for other things, we ought to
	// generalize the recursive part of this and make format detection into
	// adorable little hook functions

	if prim := data.GetPrimitive(); prim != nil {
		var strPrim *pb.String

		// Can't be an NSID if it isn't a string
		if strPrim = DataToString(data); strPrim == nil {
			// Since it's a primitive it cannot contain any other values
			// so we're done here.
			return
		}

		if nsid_re.MatchString(strPrim.Value) {
			// We'll use a custom FormatKind for Flickr stuff
			prim.FormatKind = "flickr_data"
			if prim.Formats == nil {
				prim.Formats = make(map[string]bool)
			}
			prim.Formats["flickr_nsid"] = true
			printer.Debugf("NSID Found: %+v Data: %+v\n", strPrim.Value, data)
		}
	}

	if structure := data.GetStruct(); structure != nil {
		for _, d := range structure.Fields {
			detectNSIDsData(d)
		}
	}

	if list := data.GetList(); list != nil {
		for _, d := range list.Elems {
			detectNSIDsData(d)
		}
	}

	if optional := data.GetOptional(); optional != nil {
		if optionalData, ok := optional.Value.(*pb.Optional_Data); ok {
			detectNSIDsData(optionalData.Data)
		}
	}

	if oneof := data.GetOneof(); oneof != nil {
		for _, d := range oneof.Options {
			detectNSIDsData(d)
		}
	}
}

func DetectNSIDs(method *pb.Method) {
	for _, data := range method.GetArgs() {
		detectNSIDsData(data)
	}

	for _, data := range method.GetResponses() {
		detectNSIDsData(data)
	}
}

func CreateAuthHeader(param *pb.String) *pb.Data {
	return &pb.Data{
		Value: &pb.Data_Primitive{spec_util.CategorizeString(param.Value).Obfuscate().ToProto()},
		Meta: &pb.DataMeta{
			Meta: &pb.DataMeta_Http{
				Http: &pb.HTTPMeta{
					Location: &pb.HTTPMeta_Auth{Auth: &pb.HTTPAuth{Type: pb.HTTPAuth_BEARER}},
				},
			},
		},
	}
}

func FixHTTPAuthorization(method *pb.Method) error {
	var flApiKey *pb.String

	// This double IF is strange but we need to remove the key from both places.
	if flApiKeyQS := PopQueryStringParamByName(method, "api_key"); flApiKeyQS != nil {
		// Try the query string first
		flApiKey = flApiKeyQS
	}

	if flApiKeyBody := PopBodyElementByName(method, "api_key"); flApiKeyBody != nil {
		// Then try the body
		flApiKey = flApiKeyBody
	}

	if flApiKey == nil {
		return nil
	}

	authData := CreateAuthHeader(flApiKey)

	k := ir_hash.HashDataToString(authData)

	if _, bam := method.Args[k]; bam {
		return errors.New("detected collision in data map key during plugin Transform")
	}

	method.Args[k] = authData

	printer.Debugf("Flickr Auth Data: %+v\n", authData)

	return nil
}

func FixHTTPResponseCode(method *pb.Method) error {
	var replaceCodes bool = false
	var respCode int32
	var maxCode int32
	var ok bool

	responses := method.GetResponses()

	metas := make([]*pb.DataMeta_Http, 0, len(responses))

	for _, data := range responses {

		var m *pb.DataMeta_Http

		// Skip if the response chunk isn't an http meta data chunk
		if m, ok = data.GetMeta().GetMeta().(*pb.DataMeta_Http); !ok {
			continue
		}

		metas = append(metas, m)

		if m.Http.ResponseCode > maxCode {
			maxCode = m.Http.ResponseCode
		}

		// Skip if it's not the body of the response
		if body := spec_util.HTTPBodyFromData(data); body == nil {
			continue
		}

		respCode = m.Http.ResponseCode

		var st *pb.Struct

		// Skip if it's not a Struct
		if st = data.GetStruct(); st == nil {
			continue
		}

		// There should be a field called 'stat' and if its value is 'fail' then we
		// need to replace the 200 with a 400. This is odd but we could have 4xx
		// and up responses that don't have this field, or do and we should stick
		// with the higher value code.

		// Loop over the root level fields
		for k, v := range st.Fields {
			if k != "stat" {
				continue
			}

			if s := DataToString(v); s != nil && s.Value == "fail" {
				replaceCodes = true
				respCode = 400
			}
		}
	}

	// For now, HAR recordings sometimes capture responses
	// with status code 0. Throw them away.
	if respCode == 0 {
		return errors.New(fmt.Sprintf("Throwing away request/response with 0 status\n"))
	}

	// Today, ResponseCode is sprinkled around everywhere, so we will update it everywhere.
	if replaceCodes {
		for _, m := range metas {
			m.Http.ResponseCode = respCode
		}
	}

	return nil
}

// Determine whether the transformation should be applied; for now this is a hostname check.
func (fp FlickrAkitaPlugin) IsFlickrAPICall(meta *pb.HTTPMethodMeta) bool {
	switch {
	case strings.Contains(meta.Host, "api.flickr.com"):
		return true
	default:
		return false
	}
}

/*
	Refactor notes:

	* Make a struct to store intended rewrites
	* split out PathTemplate changes, etc into bound methods on that struct
	* try to get it all in one pass

*/
func (fp FlickrAkitaPlugin) Transform(method *pb.Method) error {
	printer.Debugf("Flickr plugin.Transform called\n")

	if method.GetId().GetApiType() != pb.ApiType_HTTP_REST {
		return nil
	}

	var meta *pb.HTTPMethodMeta
	if meta = spec_util.HTTPMetaFromMethod(method); meta == nil {
		return nil
	}

	if !fp.IsFlickrAPICall(meta) {
		return errors.New(fmt.Sprintf("Discarding request not to Flickr API: %s\n", meta.Host))
	}

	printer.Debugf("Flickr PathTemplate: %+v\n", meta.PathTemplate)

	// Could be in the query string
	if flMethod := PopQueryStringParamByName(method, "method"); flMethod != nil {
		// This is Flickr's API Method, which should be mapped into an OpenAPI Path.
		// Confusingly the param name is "method" and so is Akita's request type.
		meta.PathTemplate = fmt.Sprintf("/services/awesome/%s", flMethod.Value)
		printer.Debugf("Flickr updated PathTemplate: %+v\n", meta.PathTemplate)
	}

	// Or could be in the POST/PUT body
	if flMethod := PopBodyElementByName(method, "method"); flMethod != nil {
		meta.PathTemplate = fmt.Sprintf("/services/awesome/%s", flMethod.Value)
		printer.Debugf("Flickr updated PathTemplate: %+v\n", meta.PathTemplate)
	}

	if err := FixHTTPResponseCode(method); err != nil {
		return err
	}

	if err := FixHTTPAuthorization(method); err != nil {
		return err
	}

	DetectNSIDs(method)

	printer.Debugf("Flickr PathTemplate: %+v\n", meta.PathTemplate)

	return nil
}

func LoadAkitaPlugin() (plugin.AkitaPlugin, error) {
	printer.Debugf("Flickr Plugin Loading\n")
	return FlickrAkitaPlugin{}, nil
}

func main() {}
