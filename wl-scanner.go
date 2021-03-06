package main

import (
	"bytes"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"
)

var side = flag.String("side", "client", "Either client or server")
var source = flag.String("source", "", "Where to get the XML from")
var output = flag.String("output", "", "Where to put the output go file")
var pkgName = flag.String("pkg", "wl", "Name of the package")
var unstable = flag.String("unstable", "", "Unstable suffix name to strip (e.g., v6)")

// xml types
type Protocol struct {
	XMLName    xml.Name    `xml:"protocol"`
	Name       string      `xml:"name,attr"`
	Copyright  string      `xml:"copyright"`
	Interfaces []Interface `xml:"interface"`
}

type Description struct {
	XMLName xml.Name `xml:"description"`
	Summary string   `xml:"summary,attr"`
	Text    string   `xml:",chardata"`
}

type Interface struct {
	XMLName     xml.Name    `xml:"interface"`
	Name        string      `xml:"name,attr"`
	Version     int         `xml:"version,attr"`
	Since       int         `xml:"since,attr"` // maybe in future versions
	Description Description `xml:"description"`
	Requests    []Request   `xml:"request"`
	Events      []Event     `xml:"event"`
	Enums       []Enum      `xml:"enum"`
}

type Request struct {
	XMLName     xml.Name    `xml:"request"`
	Name        string      `xml:"name,attr"`
	Type        string      `xml:"type,attr"`
	Since       int         `xml:"since,attr"`
	Description Description `xml:"description"`
	Args        []Arg       `xml:"arg"`
}

type Arg struct {
	XMLName   xml.Name `xml:"arg"`
	Name      string   `xml:"name,attr"`
	Type      string   `xml:"type,attr"`
	Interface string   `xml:"interface,attr"`
	Enum      string   `xml:"enum,attr"`
	AllowNull bool     `xml:"allow-null,attr"`
	Summary   string   `xml:"summary,attr"`
}

type Event struct {
	XMLName     xml.Name    `xml:"event"`
	Name        string      `xml:"name,attr"`
	Type        string      `xml:"type,attr"`
	Since       int         `xml:"since,attr"`
	Description Description `xml:"description"`
	Args        []Arg       `xml:"arg"`
}

type Enum struct {
	XMLName     xml.Name    `xml:"enum"`
	Name        string      `xml:"name,attr"`
	BitField    bool        `xml:"bitfield,attr"`
	Description Description `xml:"description"`
	Entries     []Entry     `xml:"entry"`
}

type Entry struct {
	XMLName xml.Name `xml:"entry"`
	Name    string   `xml:"name,attr"`
	Value   string   `xml:"value,attr"`
	Summary string   `xml:"summary,attr"`
}

// go types
type (
	GoInterface struct {
		Name        string
		WL          string
		WlInterface Interface
		Requests    []GoRequest
		Events      []GoEvent
		Enums       []GoEnum
	}

	GoRequest struct {
		Name           string
		IfaceName      string
		Params         string
		Returns        string
		Args           string
		HasNewId       bool
		NewIdInterface string
		Prefix         string
		Order          int
		Summary        string
		Description    string
	}

	GoEvent struct {
		WL        string
		Name      string
		IfaceName string
		PName     string
		EName     string
		Args      []GoArg
	}

	GoArg struct {
		Name      string
		Type      string
		PName     string
		BufMethod string
		AllowNull bool
		BasicType bool
		SnakeName string
	}

	GoEnum struct {
		Name      string
		IfaceName string
		Entries   []GoEntry
	}

	GoEntry struct {
		Name  string
		Value string
	}
)

var (
	wlTypes map[string]string = map[string]string{
		"int":    "int32",
		"uint":   "uint32",
		"string": "string",
		"fd":     "uintptr",
		"fixed":  "float32",
		"array":  "[]int32",
	}

	// sync with event.go
	bufTypesMap map[string]string = map[string]string{
		"int32":   "Int32()",
		"uint32":  "Uint32()",
		"string":  "String()",
		"float32": "Float32()",
		"[]int32": "Array()",
		"uintptr": "FD()",
	}

	wlNames    map[string]string
	fileBuffer = &bytes.Buffer{}
)

func sourceData() io.Reader {
	if *source == "" {
		log.Fatal("Must specify a -source")
	}

	if strings.HasPrefix(*source, "http:") || strings.HasPrefix(*source, "https:") {
		resp, err := http.Get(*source)
		if err != nil {
			log.Fatal(err)
		}
		return resp.Body
	} else {
		f, err := os.Open(*source)
		if err != nil {
			log.Fatal(err)
		}
		return f
	}
}

var wlPrefix string

func main() {
	log.SetFlags(0)
	flag.Parse()

	dest := *output
	if dest == "" {
		log.Fatal("Must specify -output")
	}

	mode := *side
	if !(mode == "client" || mode == "server") {
		log.Fatal("Invalid side: ", mode)
	}

	var protocol Protocol

	file := sourceData()

	err := decodeWlXML(file, &protocol)
	if err != nil {
		log.Fatal(err)
	}

	wlNames = make(map[string]string)
	wlPrefix = ""

	if protocol.Name != "wayland" {
		for _, inherit := range inheritedNames {
			wlNames[inherit] = "wl." + CamelCase(inherit)
		}
	}
	if *pkgName != "wl" {
		wlPrefix = "wl."
		trimPrefix = *pkgName + "_"
	}
	if *unstable != "" {
		ifTrimSuffix = "_" + *unstable
	}

	// required for request and event parameters
	for _, iface := range protocol.Interfaces {
		caseAndRegister(stripUnstable(iface.Name))
	}

	fmt.Fprintf(fileBuffer, "// package %s acts as a client for the %s wayland protocol.\n\n",
		*pkgName,
		protocol.Name)

	fmt.Fprintf(fileBuffer, "// generated by wl-scanner\n// https://github.com/malcolmstill/wl-scanner\n")
	fmt.Fprintf(fileBuffer, "// from: %s\n", *source)
	t := time.Now()
	fmt.Fprintf(fileBuffer, "// on %s\n", t.Format("2006-01-02 15:04:05 -0700"))
	fmt.Fprintf(fileBuffer, "package %s\n", *pkgName)
	fmt.Fprintf(fileBuffer, "import (\n")
	fmt.Fprintf(fileBuffer, "     \"sync\"\n")
	if *pkgName != "wl" {
		fmt.Fprintf(fileBuffer, "     \"github.com/malcolmstill/wl\"\n")
	}
	fmt.Fprintf(fileBuffer, ")\n")

	for _, iface := range protocol.Interfaces {
		goIface := GoInterface{
			Name:        wlNames[stripUnstable(iface.Name)],
			WlInterface: iface,
			WL:          wlPrefix,
		}

		if mode == "client" {
			rs := make([]RoE, len(goIface.WlInterface.Requests))
			for i, v := range goIface.WlInterface.Requests {
				rs[i] = RoE(v)
			}

			es := make([]RoE, len(goIface.WlInterface.Events))
			for i, v := range goIface.WlInterface.Events {
				es[i] = RoE(v)
			}

			goIface.ProcessEvents(es)
			goIface.Constructor()
			goIface.ProcessRequests(rs)
			goIface.ProcessEnums()
		} else {
			rs := make([]RoE, len(goIface.WlInterface.Requests))
			for i, v := range goIface.WlInterface.Requests {
				rs[i] = RoE(v)
			}

			es := make([]RoE, len(goIface.WlInterface.Events))
			for i, v := range goIface.WlInterface.Events {
				es[i] = RoE(v)
			}

			goIface.ProcessEvents(rs)
			goIface.Constructor()
			goIface.ProcessRequests(es)
			goIface.ProcessEnums()
		}
	}

	out, err := os.Create(dest)
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	fileBuffer.WriteTo(out)
	fmtFile()
}

func decodeWlXML(file io.Reader, prot *Protocol) error {
	err := xml.NewDecoder(file).Decode(&prot)
	if err != nil {
		return fmt.Errorf("Cannot decode wayland.xml: %s", err)
	}
	return nil
}

// register names to map
func caseAndRegister(wlName string) string {
	var orj string = wlName
	wlName = CamelCase(wlName)
	wlNames[orj] = wlName
	return wlName
}

func executeTemplate(name string, tpl string, data interface{}) {
	tmpl := template.Must(template.New(name).Parse(tpl))
	err := tmpl.Execute(fileBuffer, data)
	if err != nil {
		log.Fatal(err)
	}
}

func (i *GoInterface) Constructor() {
	executeTemplate("InterfaceTypeTemplate", ifaceTypeTemplate, i)
	if *side == "client" {
		executeTemplate("InterfaceConstructorTemplate", ifaceConstructorTemplate, i)
	} else {
		executeTemplate("InterfaceConstructorTemplate", ifaceConstructorTemplateId, i)
	}
}

type RoE interface {
	NameStr() string
	Desc() Description
	Argss() []Arg
}

func (e Event) NameStr() string {
	return e.Name
}

func (e Event) Desc() Description {
	return e.Description
}

func (e Event) Argss() []Arg {
	return e.Args
}

func (r Request) NameStr() string {
	return r.Name
}

func (r Request) Desc() Description {
	return r.Description
}

func (r Request) Argss() []Arg {
	return r.Args
}

func (i *GoInterface) ProcessRequests(roe []RoE) {
	for order, wlReq := range roe {
		var (
			returns         []string
			params          []string
			sendRequestArgs []string // for sendRequest
		)

		req := GoRequest{
			Name:        CamelCase(wlReq.NameStr()),
			IfaceName:   stripUnstable(i.Name),
			Order:       order,
			Summary:     wlReq.Desc().Summary,
			Description: reflow(wlReq.Desc().Text),
		}

		for _, arg := range wlReq.Argss() {
			var name string
			if arg.Name == "interface" {
				name = "iface"
			} else {
				name = arg.Name
			}

			if arg.Type == "new_id" {
				if arg.Interface != "" {
					newIdIface := wlNames[stripUnstable(arg.Interface)]
					components := strings.Split(newIdIface, ".")
					prefix := ""

					if len(components) > 1 {
						prefix = strings.Join(components[:len(components)-1], ".") + "."
					}

					req.NewIdInterface = components[len(components)-1]
					req.Prefix = prefix
					sendRequestArgs = append(params, wlPrefix+"Proxy(ret)")
					req.HasNewId = true

					returns = append(returns, "*"+newIdIface)
				} else { //special for registry.Bind
					sendRequestArgs = append(sendRequestArgs, "iface")
					sendRequestArgs = append(sendRequestArgs, "version")
					sendRequestArgs = append(sendRequestArgs, name)

					params = append(params, "iface string")
					params = append(params, "version uint32")
					params = append(params, fmt.Sprintf("%s %sProxy", name, wlPrefix))
				}
			} else if arg.Type == "object" && arg.Interface != "" {
				paramTypeName := wlNames[stripUnstable(arg.Interface)]
				params = append(params, fmt.Sprintf("%s *%s", name, paramTypeName))
				sendRequestArgs = append(sendRequestArgs, name)
				/*} else if arg.Type == "uint" && arg.Enum != "" {
					params = append(params, fmt.Sprintf("%s %s", arg.Name, enumArgName(ifaceName, arg.Enum)))
				}*/
			} else {
				sendRequestArgs = append(sendRequestArgs, name)
				params = append(params, fmt.Sprintf("%s %s", name, wlTypes[arg.Type]))
			}
		}

		req.Params = strings.Join(params, ",")

		if len(sendRequestArgs) > 0 {
			req.Args = "," + strings.Join(sendRequestArgs, ",")
		}

		if len(returns) > 0 { // ( ret , error )
			req.Returns = fmt.Sprintf("(%s , error)", strings.Join(returns, ","))
		} else { // returns only error
			req.Returns = "error"
		}

		executeTemplate("RequestTemplate", requestTemplate, req)
		i.Requests = append(i.Requests, req)
	}
}

func (i *GoInterface) ProcessEvents(roes []RoE) {
	// Event struct types
	for _, wlEv := range roes {
		ev := GoEvent{
			Name:      CamelCase(wlEv.NameStr()),
			PName:     snakeCase(wlEv.NameStr()),
			IfaceName: i.Name,
			WL:        wlPrefix,
		}

		ev.EName = i.Name + ev.Name

		for _, arg := range wlEv.Argss() {
			goarg := GoArg{
				Name:      CamelCase(arg.Name),
				PName:     snakeCase(arg.Name),
				AllowNull: arg.AllowNull,
			}
			if t, ok := wlTypes[arg.Type]; ok { // if basic type
				bufMethod, ok := bufTypesMap[t]
				if !ok {
					log.Printf("%s not registered", t)
				} else {
					if bufMethod == "FD()" {
						goarg.BufMethod = "p.Context().NextFD()"
					} else {
						goarg.BufMethod = "event." + bufMethod
					}
				}
				/*
					if arg.Type == "uint" && arg.Enum != "" { // enum type
						enumTypeName := ifaceName + CamelCase(arg.Enum)
						fmt.Fprintf(&eventBuffer, "%s %s\n", CamelCase(arg.Name), enumTypeName)
					} else {
						fmt.Fprintf(&eventBuffer, "%s %s\n", CamelCase(arg.Name), t)
					}*/
				goarg.Type = t
				goarg.BasicType = true
			} else { // interface type
				if arg.Type == "new_id" && *side == "server" && arg.Interface != "" {
					t = "*" + wlNames[stripUnstable(arg.Interface)]
					components := strings.Split(wlNames[stripUnstable(arg.Interface)], ".")
					prefix := ""

					if len(components) > 1 {
						prefix = strings.Join(components[:len(components)-1], ".") + "."
					}

					goarg.BufMethod = fmt.Sprintf("%sNew%s(p.Context(), int(event.Uint32()))", prefix, components[len(components)-1])
				} else if arg.Type == "new_id" && arg.Interface != "" {
					t = "*" + wlNames[stripUnstable(arg.Interface)]
					goarg.BufMethod = fmt.Sprintf("event.Proxy(p.Context()).(%s)", t)
				} else if arg.Type == "object" && arg.AllowNull == false && arg.Interface != "" {
					t = "*" + wlNames[stripUnstable(arg.Interface)]
					goarg.BufMethod = fmt.Sprintf("event.Proxy(p.Context()).(%s)", t)
				} else if arg.Type == "object" && arg.AllowNull == true && arg.Interface != "" {
					t = "*" + wlNames[stripUnstable(arg.Interface)]
					goarg.BufMethod = "event.Proxy(p.Context())"
					goarg.Type = t
					goarg.SnakeName = strings.ToLower(goarg.Name)
				} else {
					t = wlPrefix + "Proxy"
					goarg.BufMethod = "event." + wlPrefix + "Proxy(p.Context())"
				}
				goarg.Type = t
				goarg.BasicType = false
			}

			ev.Args = append(ev.Args, goarg)
		}

		executeTemplate("EventTemplate", eventTemplate, ev)
		executeTemplate("AddRemoveHandlerTemplate", ifaceAddRemoveHandlerTemplate, ev)

		i.Events = append(i.Events, ev)
	}

	if len(i.Events) > 0 {
		executeTemplate("InterfaceDispatchTemplate", ifaceDispatchTemplate, i)
	}
}

func (i *GoInterface) ProcessEnums() {
	// Enums - Constants
	for _, wlEnum := range i.WlInterface.Enums {
		goEnum := GoEnum{
			Name:      CamelCase(wlEnum.Name),
			IfaceName: i.Name,
		}

		for _, wlEntry := range wlEnum.Entries {
			goEntry := GoEntry{
				Name:  CamelCase(wlEntry.Name),
				Value: wlEntry.Value,
			}
			goEnum.Entries = append(goEnum.Entries, goEntry)
		}

		executeTemplate("InterfaceEnumsTemplate", ifaceEnums, goEnum)
	}
}

/*
func enumArgName(ifaceName, enumName string) string {
	if strings.Index(enumName, ".") == -1 {
		return ifaceName + CamelCase(enumName)
	}

	parts := strings.Split(enumName, ".")
	if len(parts) != 2 {
		log.Fatalf("enum args must be \"interface.enum\" format: we get %s",enumName)
	}
	return CamelCase(parts[0]) + CamelCase(parts[1])
}
*/

var trimPrefix = "wl_"
var ifTrimSuffix = ""

func CamelCase(wlName string) string {
	wlName = strings.TrimPrefix(wlName, trimPrefix)

	// replace all "_" chars to " " chars
	wlName = strings.Replace(wlName, "_", " ", -1)

	// Capitalize first chars
	wlName = strings.Title(wlName)

	// remove all spaces
	wlName = strings.Replace(wlName, " ", "", -1)

	return wlName
}

func snakeCase(wlName string) string {
	if strings.HasPrefix(wlName, "wl_") {
		wlName = strings.TrimPrefix(wlName, "wl_")
	}

	// replace all "_" chars to " " chars
	wlName = strings.Replace(wlName, "_", " ", -1)
	parts := strings.Split(wlName, " ")
	for i, p := range parts {
		if i == 0 {
			continue
		}
		parts[i] = strings.Title(p)
	}

	return strings.Join(parts, "")
}

func fmtFile() {
	goex, err := exec.LookPath("go")
	if err != nil {
		log.Printf("go executable cannot found run \"go fmt %s\" yourself: %s", *output, err)
		return
	}

	cmd := exec.Command(goex, "fmt", *output)
	er2 := cmd.Run()
	if er2 != nil {
		log.Fatalf("Cannot run cmd: %s", er2)
	}
}

// templates
var (
	ifaceTypeTemplate = `
type {{.Name}} struct {
	{{.WL}}BaseProxy
	{{- if gt (len .Events) 0 }}
	mu sync.RWMutex
	{{- end}}

	{{- range .Events}}
	{{.PName}}Handlers []{{.EName}}Handler
	{{- end}}
}
`

	ifaceConstructorTemplate = `
func New{{.Name}}(ctx *{{.WL}}Context) *{{.Name}} {
	ret := new({{.Name}})
	ctx.Register(ret)
	return ret
}
`
	ifaceConstructorTemplateId = `
func New{{.Name}}(ctx *{{.WL}}Context, id int) *{{.Name}} {
	ret := new({{.Name}})
	ctx.RegisterId(ret, id)
	return ret
}
`

	ifaceAddRemoveHandlerTemplate = `
func (p *{{.IfaceName}}) Add{{.Name}}Handler(h {{.EName}}Handler) {
	if h != nil {
		p.mu.Lock()
		p.{{.PName}}Handlers = append(p.{{.PName}}Handlers , h)
		p.mu.Unlock()
	}
}

func (p *{{.IfaceName}}) Remove{{.Name}}Handler(h {{.EName}}Handler) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i , e := range p.{{.PName}}Handlers {
		if e == h {
			p.{{.PName}}Handlers = append(p.{{.PName}}Handlers[:i] , p.{{.PName}}Handlers[i+1:]...)
			break
		}
	}
}
`

	requestTemplate = `
// {{.Name}} will {{.Summary}}.
//
	{{- if .HasNewId}}

		{{- if gt (len .Params) 0 }}
{{.Description}}func (p *{{.IfaceName}}) {{.Name}}({{.Params}}, id int) {{.Returns}} {
		{{- else}}
{{.Description}}func (p *{{.IfaceName}}) {{.Name}}(id int) {{.Returns}} {
		{{- end}}
	ret := {{.Prefix}}New{{.NewIdInterface}}(p.Context(), id)
	return ret , p.Context().SendRequest(p,{{.Order}}{{.Args}})
	{{- else}}
{{.Description}}func (p *{{.IfaceName}}) {{.Name}}({{.Params}}) {{.Returns}} {
	return p.Context().SendRequest(p,{{.Order}}{{.Args}})
	{{- end}}
}
`

	eventTemplate = `
type {{.IfaceName}}{{.Name}}Event struct {
	{{- range .Args }}
	{{.Name}} {{.Type}}
	{{- end }}
}

type {{.IfaceName}}{{.Name}}Handler interface {
    Handle{{.EName}}({{.EName}}Event)
}
`

	ifaceDispatchTemplate = `
func (p *{{.Name}}) Dispatch(event *{{.WL}}Event) {
	{{- $ifaceName := .Name }}
	switch event.Opcode {
	{{- range $i , $event := .Events }}
	case {{$i}}:
		if len(p.{{.PName}}Handlers) > 0 {
			ev := {{$ifaceName}}{{.Name}}Event{}
			{{- range $event.Args}}
			{{if and (not .BasicType) (.AllowNull) -}}
			{{.SnakeName}} := {{.BufMethod}}
			if {{.SnakeName}} != nil {
				ev.{{.Name}} = {{.SnakeName}}.({{.Type}})
			}
			{{- else -}}
			ev.{{.Name}} = {{.BufMethod}}
			{{- end }}
			{{- end}}
			p.mu.RLock()
			for _, h := range p.{{.PName}}Handlers {
				h.Handle{{.EName}}(ev)
			}
			p.mu.RUnlock()
		}
	{{- end}}
	}
}
`

	ifaceEnums = `
const (
	{{- $ifaceName := .IfaceName }}
	{{- $enumName := .Name }}
	{{- range .Entries}}
	{{$ifaceName}}{{$enumName}}{{.Name}} = {{.Value}}
	{{- end}}
)
`
)

var inheritedNames = []string{
	"wl_display",
	"wl_registry",
	"wl_callback",
	"wl_compositor",
	"wl_shm_pool",
	"wl_shm",
	"wl_buffer",
	"wl_data_offer",
	"wl_data_source",
	"wl_data_device",
	"wl_data_device_manager",
	"wl_shell",
	"wl_shell_surface",
	"wl_surface",
	"wl_seat",
	"wl_pointer",
	"wl_keyboard",
	"wl_touch",
	"wl_output",
	"wl_region",
	"wl_subcompositor",
	"wl_subsurface",
}

func reflow(text string) string {
	ret := ""
	for _, line := range strings.Split(text, "\n") {
		ret = ret + "// " + strings.TrimSpace(line) + "\n"
	}
	return ret
}

func stripUnstable(ifname string) string {
	return strings.TrimSuffix(ifname, ifTrimSuffix)
}
