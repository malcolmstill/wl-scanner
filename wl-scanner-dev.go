package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"log"
	"os"
	"strings"
	"text/template"
)

// xml types
type (
	Protocol struct {
		XMLName    xml.Name    `xml:"protocol"`
		Name       string      `xml:"name,attr"`
		Copyright  string      `xml:"copyright"`
		Interfaces []Interface `xml:"interface"`
	}

	Description struct {
		XMLName     xml.Name `xml:"description"`
		Summary     string   `xml:"summary,attr"`
		Description string   `xml:"description"`
	}

	Interface struct {
		XMLName     xml.Name    `xml:"interface"`
		Name        string      `xml:"name,attr"`
		Version     int         `xml:"version,attr"`
		Since       int         `xml:"since,attr"` // maybe in future versions
		Description Description `xml:"description"`
		Requests    []Request   `xml:"request"`
		Events      []Event     `xml:"event"`
		Enums       []Enum      `xml:"enum"`
	}

	Request struct {
		XMLName     xml.Name    `xml:"request"`
		Name        string      `xml:"name,attr"`
		Type        string      `xml:"type,attr"`
		Since       int         `xml:"since,attr"`
		Description Description `xml:"description"`
		Args        []Arg       `xml:"arg"`
	}

	Arg struct {
		XMLName   xml.Name `xml:"arg"`
		Name      string   `xml:"name,attr"`
		Type      string   `xml:"type,attr"`
		Interface string   `xml:"interface,attr"`
		Enum      string   `xml:"enum,attr"`
		AllowNull bool     `xml:"allow-null,attr"`
		Summary   string   `xml:"summary,attr"`
	}

	Event struct {
		XMLName     xml.Name    `xml:"event"`
		Name        string      `xml:"name,attr"`
		Since       int         `xml:"since,attr"`
		Description Description `xml:"description"`
		Args        []Arg       `xml:"arg"`
	}

	Enum struct {
		XMLName     xml.Name    `xml:"enum"`
		Name        string      `xml:"name,attr"`
		BitField    bool        `xml:"bitfield,attr"`
		Description Description `xml:"description"`
		Entries     []Entry     `xml:"entry"`
	}

	Entry struct {
		XMLName xml.Name `xml:"entry"`
		Name    string   `xml:"name,attr"`
		Value   string   `xml:"value,attr"`
		Summary string   `xml:"summary,attr"`
	}
)

// go types
type (
	// represents all file
	GoProtocol struct {
		buffer     bytes.Buffer
		Interfaces []GoInterface
	}

	GoInterface struct {
		Name        string
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
		Order          int
	}

	GoEvent struct {
		Name      string
		IfaceName string
		PName     string
		Args      []GoArg
	}

	GoArg struct {
		Name      string
		Type      string
		PName     string
		BufMethod string
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

	wlNames map[string]string
	goProt  GoProtocol
)

func init() {
	log.SetFlags(0)
}

func main() {
	var protocol Protocol

	file, err := os.Open("wayland.xml")
	if err != nil {
		log.Fatal(err)
	}

	err = decodeWlXML(file, &protocol)
	if err != nil {
		log.Fatal(err)
	}

	wlNames = make(map[string]string)

	// required for request and event parameters
	for _, iface := range protocol.Interfaces {
		caseAndRegister(iface.Name)
	}

	for _, iface := range protocol.Interfaces {
		goIface := GoInterface{
			Name:        wlNames[iface.Name],
			WlInterface: iface,
		}

		goIface.ProcessEvents()
		goIface.Constructor()
		goIface.ProcessRequests()
		goIface.ProcessEnums()

		//goProt.Interfaces = append(goProt.Interfaces, goIface)
	}
}

func decodeWlXML(file *os.File, prot *Protocol) error {
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

func (i *GoInterface) Constructor() {
	tmpl := template.Must(template.New("InterfaceTypeTemplate").Parse(ifaceTypeTemplate))
	err := tmpl.Execute(os.Stdout, i)
	if err != nil {
		log.Fatal(err)
	}

	tmpl = template.Must(template.New("InterfaceConstructorTemplate").Parse(ifaceConstructorTemplate))
	err = tmpl.Execute(os.Stdout, i)
	if err != nil {
		log.Fatal(err)
	}

	if len(i.Events) > 0 {
		tmpl = template.Must(template.New("InterfaceDisposeTemplate").Parse(ifaceDisposeTemplate))
		err = tmpl.Execute(os.Stdout, i)
		if err != nil {
			log.Fatal(err)
		}

		tmpl = template.Must(template.New("InterfaceHandleEventsTemplate").Parse(ifaceHandleEventsTemplate))
		err = tmpl.Execute(os.Stdout, i)
		if err != nil {
			log.Fatal(err)
		}
	}
}

func (i *GoInterface) ProcessRequests() {
	for order, wlReq := range i.WlInterface.Requests {
		var (
			returns         []string
			params          []string
			sendRequestArgs []string // for sendRequest
		)

		req := GoRequest{
			Name:      CamelCase(wlReq.Name),
			IfaceName: i.Name,
			Order:     order,
		}

		/* TODO request kodlarını sabit olarak tanımla
		reqCodeName := strings.ToTitle(fmt.Sprintf("_%s_%s", i.Name , req.Name)) // first _ for not export constant
		"%s = %d", reqCodeName, order)
		*/

		for _, arg := range wlReq.Args {
			if arg.Type == "new_id" {
				if arg.Interface != "" {
					newIdIface := wlNames[arg.Interface]
					req.NewIdInterface = newIdIface
					sendRequestArgs = append(params, "Proxy(ret)")
					req.HasNewId = true

					returns = append(returns, "*"+newIdIface)
				} else { //special for registry.Bind
					sendRequestArgs = append(sendRequestArgs, "iface")
					sendRequestArgs = append(sendRequestArgs, "version")
					sendRequestArgs = append(sendRequestArgs, arg.Name)

					params = append(params, "iface string")
					params = append(params, "version uint32")
					params = append(params, fmt.Sprintf("%s Proxy", arg.Name))
				}
			} else if arg.Type == "object" && arg.Interface != "" {
				paramTypeName := wlNames[arg.Interface]
				params = append(params, fmt.Sprintf("%s *%s", arg.Name, paramTypeName))
				/*} else if arg.Type == "uint" && arg.Enum != "" {
					params = append(params, fmt.Sprintf("%s %s", arg.Name, enumArgName(ifaceName, arg.Enum)))
				}*/
			} else {
				sendRequestArgs = append(sendRequestArgs, arg.Name)
				params = append(params, fmt.Sprintf("%s %s", arg.Name, wlTypes[arg.Type]))
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

		tmpl := template.Must(template.New("RequestTemplate").Parse(requestTemplate))
		err := tmpl.Execute(os.Stdout, req)
		if err != nil {
			log.Fatal(err)
		}

		i.Requests = append(i.Requests, req)
	}
}

func (i *GoInterface) ProcessEvents() {
	// Event struct types
	for _, wlEv := range i.WlInterface.Events {
		ev := GoEvent{
			Name:  CamelCase(wlEv.Name),
			PName: snakeCase(wlEv.Name),
			//WlEvent:   wlEv,
			IfaceName: i.Name,
		}

		for _, arg := range wlEv.Args {
			goarg := GoArg{
				Name:  CamelCase(arg.Name),
				PName: snakeCase(arg.Name),
			}
			if t, ok := wlTypes[arg.Type]; ok { // if basic type
				bufMethod, ok := bufTypesMap[t]
				if !ok {
					log.Printf("%s not registered", t)
				} else {
					goarg.BufMethod = bufMethod
				}
				/*
					if arg.Type == "uint" && arg.Enum != "" { // enum type
						enumTypeName := ifaceName + CamelCase(arg.Enum)
						fmt.Fprintf(&eventBuffer, "%s %s\n", CamelCase(arg.Name), enumTypeName)
					} else {
						fmt.Fprintf(&eventBuffer, "%s %s\n", CamelCase(arg.Name), t)
					}*/
				goarg.Type = t
			} else { // interface type
				if (arg.Type == "object" || arg.Type == "new_id") && arg.Interface != "" {
					t = "*" + wlNames[arg.Interface]
					goarg.BufMethod = fmt.Sprintf("Proxy(p.Context()).(%s)", t)
				} else {
					t = "Proxy"
					goarg.BufMethod = "Proxy(p.Context())"
				}
				goarg.Type = t
			}

			ev.Args = append(ev.Args, goarg)
		}

		tmpl := template.Must(template.New("EventTemplate").Parse(eventTemplate))
		err := tmpl.Execute(os.Stdout, ev)
		if err != nil {
			log.Fatal(err)
		}

		i.Events = append(i.Events, ev)
	}

	if len(i.Events) > 0 {
		tmpl := template.Must(template.New("InterfaceDispatchTemplate").Parse(ifaceDispatchTemplate))
		err := tmpl.Execute(os.Stdout, i)
		if err != nil {
			log.Fatal(err)
		}
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

		tmpl := template.Must(template.New("InterfaceEnumsTemplate").Parse(ifaceEnums))
		err := tmpl.Execute(os.Stdout, goEnum)
		if err != nil {
			log.Fatal(err)
		}
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

func CamelCase(wlName string) string {
	if strings.HasPrefix(wlName, "wl_") {
		wlName = strings.TrimPrefix(wlName, "wl_")
	}

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

// templates
var (
	ifaceTypeTemplate = `
type {{.Name}} struct {
	BaseProxy
	{{- if gt (len .Events) 0 }}
	mu sync.Mutex
	{{- end}}

	{{- $ifaceName := .Name }}
	{{- range .Events}}
	{{.PName}}Chan chan {{$ifaceName}}{{.Name}}Event
	On{{.Name}} func({{$ifaceName}}{{.Name}}Event)
	{{- end}}

	{{- if gt (len .Events) 0 }}
	disposeChan chan struct{}
	{{- end}}
}
`

	ifaceConstructorTemplate = `
func New{{.Name}}(ctx *Context) *{{.Name}} {
	ret := new({{.Name}})

	{{- $ifaceName := .Name }}
	{{- range .Events}}
	ret.{{.PName}}Chan = make(chan {{$ifaceName}}{{.Name}}Event)
	{{- end}}

	{{- if gt (len .Events) 0}}
	ret.disposeChan = make(chan struct{})
	go ret.handleEvents()
	{{- end }}
	ctx.register(ret)
	return ret
}
`
	ifaceDisposeTemplate = `
func (p *{{.Name}}) Dispose() {
	p.disposeChan <- struct{}{}
}
`

	ifaceHandleEventsTemplate = `
func (p *{{.Name}}) handleEvents() {
loop:
	for {
		select {
		{{- range .Events}}
		case ev := <-p.{{.PName}}Chan:
			mu.Lock()
			if p.On{{.Name}} != nil {
				p.On{{.Name}}(ev)
			}
			mu.Unlock()
		{{- end}}
		case <-p.disposeChan:
			break loop
		}
	}
}
`

	requestTemplate = `
func (p *{{.IfaceName}}) {{.Name}}({{.Params}}) {{.Returns}} { 
	{{- if .HasNewId}}
	ret := New{{.NewIdInterface}}(p.Context())
	return ret , p.Context().sendRequest(p,{{.Order}}{{.Args}})
	{{- else}}
	return p.Context().sendRequest(p,{{.Order}}{{.Args}})
	{{- end}}
}
`

	eventTemplate = `
type {{.IfaceName}}{{.Name}}Event struct {
	{{- range .Args }}
	{{.Name}} {{.Type}}
	{{- end }}
}
`
	ifaceDispatchTemplate = `
func (p *{{.Name}}) Dispatch(event *Event) {
	{{- $ifaceName := .Name }}
	switch event.opcode {
	{{- range $i , $event := .Events }}
	case {{$i}}:
		ev := new({{$ifaceName}}{{.Name}}Event)
		{{- range $event.Args}}
		ev.{{.PName}} = event.{{.BufMethod}}
		{{- end}}
		p.{{.PName}}Chan<-ev
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