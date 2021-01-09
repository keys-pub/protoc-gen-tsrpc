package gentsrpc

import (
	"bytes"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"

	"github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway/descriptor"
)

func removePackage(s string) string {
	sp := strings.Split(s, ".")
	return sp[len(sp)-1]
}

// Lowers first uppercase characters (Foo => foo, FOOBar => fooBar)
func lowerPrefix(s string) (lower string) {
	for pos, char := range s {
		if unicode.IsUpper(char) {
			lower = lower + string(unicode.ToLower(char))
		} else {
			if pos > 1 {
				lower = lower[:len(lower)-1] + s[pos-1:]
			} else {
				lower = lower + s[pos:]
			}
			break
		}
	}
	return
}

func (cfg GeneratorOptions) methodToPromise(name string, m *descriptor.Method) (string, string, []string) {
	// serviceName := m.Service.GetName()
	methodName := *m.Name
	rpcName := lowerPrefix(methodName)
	requestType := (*m.InputType)[1:]
	responseType := (*m.OutputType)[1:]
	types := []string{requestType, responseType}
	// fullMethod := serviceName + "." + methodName

	s := `  ` + rpcName + `(req: ` + requestType + `): Promise<` + responseType + `> {
    return new Promise<` + responseType + `>((resolve, reject) => {
      this.log.info('` + rpcName + `')
      this.service().` + rpcName + `(req, (err: RPCError, resp: ` + responseType + `) => {
        if (err) {
          reject(err)
          this.emitError(err)
          return
        }
        resolve(resp)
      })		
    })
  }
`
	return s, methodName, types
}

func (cfg GeneratorOptions) streamMethod(name string, m *descriptor.Method) (string, string, []string) {
	// serviceName := m.Service.GetName()
	methodName := *m.Name
	rpcName := lowerPrefix(methodName)
	requestType := (*m.InputType)[1:]
	responseType := (*m.OutputType)[1:]
	types := []string{requestType, responseType}
	// fullMethod := serviceName + "." + methodName

	s := `  ` + rpcName + `(): ClientDuplexStream<` + requestType + `, ` + responseType + `> {
    this.log.info('` + rpcName + `')
    return this.service().` + rpcName + `()
  }
`
	return s, methodName, types
}

func (cfg GeneratorOptions) readStreamMethod(name string, m *descriptor.Method) (string, string, []string) {
	// serviceName := m.Service.GetName()
	methodName := *m.Name
	rpcName := lowerPrefix(methodName)
	requestType := (*m.InputType)[1:]
	responseType := (*m.OutputType)[1:]
	types := []string{requestType, responseType}
	// fullMethod := serviceName + "." + methodName

	s := `  ` + rpcName + `(req: ` + requestType + `): ClientReadableStream<` + responseType + `> {
    this.log.info('` + rpcName + `')
    return this.service().` + rpcName + `(req)
  }
`
	return s, methodName, types
}

func (cfg GeneratorOptions) serviceToRPC(packageName string, s *descriptor.Service, reg *descriptor.Registry) (string, []string, error) {
	types := []string{}
	result := []string{}
	methods := []string{}
	for _, m := range s.Methods {
		var ipc string
		var method string
		var typs []string
		if m.ClientStreaming != nil && *m.ClientStreaming && m.ServerStreaming != nil && *m.ServerStreaming {
			ipc, method, typs = cfg.streamMethod(*s.Name, m)
		} else if m.ServerStreaming != nil && *m.ServerStreaming {
			ipc, method, typs = cfg.readStreamMethod(*s.Name, m)
			// } else if m.ClientStreaming != nil && *m.ClientStreaming {
			// 	return "", nil, errors.Errorf("server streaming unsupported: %s", *s.Name)
		} else {
			ipc, method, typs = cfg.methodToPromise(*s.Name, m)
		}

		result = append(result, ipc)
		methods = append(methods, method)
		types = append(types, typs...)
	}
	types = unique(types)

	out := `export class ` + *s.Name + `Service extends EventEmitter {
  serviceFn: () => ServiceClient
  client?: ServiceClient
  log: Logger = dummyLogger
  
  constructor(serviceFn: () => ServiceClient) {
    super()
    this.serviceFn = serviceFn
  }

  service(): ServiceClient {
    if (!this.client) {
      this.client = this.serviceFn()
    }
    return this.client
  }

  emitError(err: RPCError) {
    switch (err.code) {
      case grpc.status.PERMISSION_DENIED:
      case grpc.status.UNAUTHENTICATED:
        this.emit('unauthenticated', err)
        break
      case grpc.status.UNAVAILABLE:
        this.emit('unavailable', err)
        break
      default:
        this.emit('error', err)
        break
	}
  }

` + strings.Join(result, "\n") + `}`

	return out, types, nil
}

func unique(strs []string) []string {
	keys := make(map[string]bool)
	list := []string{}
	for _, entry := range strs {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

func generate(file *descriptor.File, registry *descriptor.Registry, options GeneratorOptions) (string, error) {
	out := []string{}
	types := []string{}
	f, err := registry.LookupFile(file.GetName())
	if err != nil {
		return "", err
	}

	name := file.GetName()
	ext := filepath.Ext(name)
	pkg := file.GetPackage()
	base := strings.TrimSuffix(name, ext)
	typesImport := "./" + base

	for _, service := range f.Services {
		s, t, serr := options.serviceToRPC(base, service, registry)
		if serr != nil {
			return "", serr
		}
		out = append(out, s)
		types = append(types, t...)
	}

	typesAs := []string{}
	for _, t := range types {
		typesAs = append(typesAs, t)
	}

	// typesExport := []string{}
	// for _, t := range types {
	// 	typesExport = append(typesExport, "export type "+t)
	// }

	buf := new(bytes.Buffer)
	tmpl, err := template.New("").Parse(`// Code generated by protoc-gen-tsrpc DO NOT EDIT.
// InputID: {{.InputID}}

import {ServiceClient} from '@grpc/grpc-js/build/src/make-client'
import {ClientDuplexStream, ClientReadableStream} from '@grpc/grpc-js/build/src/call'
import * as grpc from '@grpc/grpc-js'
import {EventEmitter} from 'events'
import * as ` + pkg + ` from '{{.TypesImport}}'
import {dummyLogger, Logger} from 'ts-log'

export type RPCError = {
  name: string
  message: string
  code: number
  details: string
}

{{.Out}}
`)
	if err != nil {
		return "", err
	}
	err = tmpl.Execute(buf, struct {
		GeneratorOptions
		Types       string
		TypesImport string
		// TypesExport string
		Out string
	}{
		GeneratorOptions: options,
		Types:            strings.Join(typesAs, ",\n  "),
		TypesImport:      typesImport,
		// TypesExport:      strings.Join(typesExport, "\n"),
		Out: strings.Join(out, "\n\n"),
	})
	if err != nil {
		return "", err
	}
	return string(buf.Bytes()), nil
}
