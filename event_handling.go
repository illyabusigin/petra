package petra

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/iancoleman/strcase"
	"github.com/jfyne/live/page"
)

type eventHandler struct {
	id    string
	funcs map[string]*reflect.Method
}

func newEventHandler(id string) *eventHandler {
	return &eventHandler{
		id:    id,
		funcs: map[string]*reflect.Method{},
	}
}

// {{on "click" (fn this.handleClick)}}
type handler struct {
	directive string
	function  string
}

func (h handler) fn() string {
	return strcase.ToKebab(h.function)
}

func newHandler(val string) handler {
	directiveRegex := regexp.MustCompile(`"([^"]*)"`)
	directive := string(directiveRegex.Find([]byte(val)))
	directive = strings.Trim(directive, `"`)

	fnRegex := regexp.MustCompile(`\(fn ([^}]+)\)`)
	fn := string(fnRegex.Find([]byte(val)))
	fn = strings.TrimPrefix(fn, "(fn ")
	fn = strings.TrimSuffix(fn, ")")
	fn = strings.ReplaceAll(fn, "this.", "")

	// TODO: Add error handling
	return handler{
		directive: directive,
		function:  strcase.ToCamel(fn),
	}
}

func (r *eventHandler) registerHandlers(self interface{}, c *page.Component) error {
	componentType := reflect.TypeOf(self)
	for i := 0; i < componentType.NumMethod(); i++ {
		method := componentType.Method(i)
		m := reflect.ValueOf(self).MethodByName(method.Name)

		switch fn := m.Interface().(type) {
		case func(c *page.Component) page.EventHandler:
			// fmt.Println("adding event handler ", strings.ToLower(strcase.ToKebab(method.Name)), c)
			c.HandleEvent(strings.ToLower(strcase.ToKebab(method.Name)), fn(c))

			r.funcs[method.Name] = &method
			// fmt.Println("populated funcs", r.funcs)
		}
	}

	return nil
}

func (r *eventHandler) processTemplate(data string) (string, error) {
	data, err := r.handleOnEvent(data)
	if err != nil {
		return "", err
	}

	data, err = r.handleOnKeyEvent(data)
	if err != nil {
		return "", err
	}

	return data, nil
}

func (r *eventHandler) handleOnEvent(data string) (string, error) {
	// process {{on}} directives
	onRegex := regexp.MustCompile(`{{on ([^}]+)}}`)
	matching := onRegex.FindAll([]byte(data), -1)
	for _, match := range matching {
		matchedString := string(match)
		handler := newHandler(matchedString)
		_, matchingFunc := r.funcs[handler.function]

		if !matchingFunc {
			// continue
			return "", fmt.Errorf("no matching function for: <%v>, funcs %v", handler.function, r.funcs)
		}

		// fmt.Printf("handler: %#v, funcs :%v\n", handler, r.funcs)
		switch handler.directive {
		case "click":
			// fmt.Println("adding handle click", fmt.Sprintf(`live-click="%v--%v"`, r.id, handler.fn()))
			data = strings.Replace(data, matchedString, fmt.Sprintf(`live-click="%v--%v"`, r.id, handler.fn()), 1)
		case "change":
			data = strings.Replace(data, matchedString, fmt.Sprintf(`live-change="%v--%v"`, r.id, handler.fn()), 1)
		case "submit":
			data = strings.Replace(data, matchedString, fmt.Sprintf(`live-submit="%v--%v"`, r.id, handler.fn()), 1)

		default:
			return "", fmt.Errorf("unsupported directive: <%v>", handler.directive)
		}

	}

	return data, nil
}

func (r *eventHandler) handleOnKeyEvent(data string) (string, error) {
	// onRegex := regexp.MustCompile(`{{on-key ([^}]+)}}`)
	// matching := onRegex.FindAll([]byte(data), -1)
	// for _, match := range matching {
	// 	matchedString := string(match)
	// 	handler := newHandler(matchedString)
	// 	_, matchingFunc := r.funcs[handler.function]

	// 	if !matchingFunc {
	// 		// continue
	// 		return "", fmt.Errorf("no matching function for: <%v>, funcs %v", handler.function, r.funcs)
	// 	}

	// 	fmt.Printf("handler: %#v, funcs :%v\n", handler, r.funcs)
	// 	switch handler.directive {
	// 	case "click":
	// 		data = strings.Replace(data, matchedString, fmt.Sprintf(`live-key="%v--%v"`, r.id, handler.fn()), 1)
	// 	default:
	// 		return "", fmt.Errorf("unsupported directive: <%v>", handler.directive)
	// 	}

	// }

	// //   <button live-window-keyup="inc" live-key="ArrowUp" live-click="inc">+</button>

	// fmt.Println("Found matching", len(matching), string(matching[0]), r.id)

	return data, nil
}
