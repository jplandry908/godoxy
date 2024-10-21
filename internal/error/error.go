package error

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type (
	Error     = *ErrorImpl
	ErrorImpl struct {
		subject string
		err     error
		extras  []ErrorImpl
	}
	ErrorJSONMarshaller struct {
		Subject string                `json:"subject"`
		Err     string                `json:"error"`
		Extras  []ErrorJSONMarshaller `json:"extras,omitempty"`
	}
)

func From(err error) Error {
	if IsNil(err) {
		return nil
	}
	return &ErrorImpl{err: err}
}

func FromJSON(data []byte) (Error, bool) {
	var j ErrorJSONMarshaller
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, false
	}
	if j.Err == "" {
		return nil, false
	}
	extras := make([]ErrorImpl, len(j.Extras))
	for i, e := range j.Extras {
		extra, ok := fromJSONObject(e)
		if !ok {
			return nil, false
		}
		extras[i] = *extra
	}
	return &ErrorImpl{
		subject: j.Subject,
		err:     errors.New(j.Err),
		extras:  extras,
	}, true
}

func TryUnwrap(err error) error {
	if err == nil {
		return nil
	}
	if unwrapped := errors.Unwrap(err); unwrapped != nil {
		return unwrapped
	}
	return err
}

// Check is a helper function that
// convert (T, error) to (T, NestedError).
func Check[T any](obj T, err error) (T, Error) {
	return obj, From(err)
}

func Join(message string, err ...Error) Error {
	extras := make([]ErrorImpl, len(err))
	nErr := 0
	for i, e := range err {
		if e == nil {
			continue
		}
		extras[i] = *e
		nErr++
	}
	if nErr == 0 {
		return nil
	}
	return &ErrorImpl{
		err:    errors.New(message),
		extras: extras,
	}
}

func JoinE(message string, err ...error) Error {
	b := NewBuilder("%s", message)
	for _, e := range err {
		b.AddE(e)
	}
	return b.Build()
}

func IsNil(err error) bool {
	return err == nil
}

func IsNotNil(err error) bool {
	return err != nil
}

func (ne Error) String() string {
	var buf strings.Builder
	ne.writeToSB(&buf, 0, "")
	return buf.String()
}

func (ne Error) Is(err error) bool {
	if ne == nil {
		return err == nil
	}
	// return errors.Is(ne.err, err)
	if errors.Is(ne.err, err) {
		return true
	}
	for _, e := range ne.extras {
		if e.Is(err) {
			return true
		}
	}
	return false
}

func (ne Error) IsNot(err error) bool {
	return !ne.Is(err)
}

func (ne Error) Error() error {
	if ne == nil {
		return nil
	}
	return ne.buildError(0, "")
}

func (ne Error) With(s any) Error {
	if ne == nil {
		return ne
	}
	var msg string
	switch ss := s.(type) {
	case nil:
		return ne
	case *ErrorImpl:
		if len(ss.extras) == 1 {
			ne.extras = append(ne.extras, ss.extras[0])
			return ne
		}
		return ne.withError(ss)
	case error:
		// unwrap only once
		return ne.withError(From(TryUnwrap(ss)))
	case string:
		msg = ss
	case fmt.Stringer:
		return ne.appendMsg(ss.String())
	default:
		return ne.appendMsg(fmt.Sprint(s))
	}
	return ne.withError(From(errors.New(msg)))
}

func (ne Error) Extraf(format string, args ...any) Error {
	return ne.With(errorf(format, args...))
}

func (ne Error) Subject(s any, sep ...string) Error {
	if ne == nil {
		return ne
	}
	var subject string
	switch ss := s.(type) {
	case string:
		subject = ss
	case fmt.Stringer:
		subject = ss.String()
	default:
		subject = fmt.Sprint(s)
	}
	switch {
	case ne.subject == "":
		ne.subject = subject
	case len(sep) > 0:
		ne.subject = fmt.Sprintf("%s%s%s", subject, sep[0], ne.subject)
	default:
		ne.subject = fmt.Sprintf("%s > %s", subject, ne.subject)
	}
	return ne
}

func (ne Error) Subjectf(format string, args ...any) Error {
	if ne == nil {
		return ne
	}
	return ne.Subject(fmt.Sprintf(format, args...))
}

func (ne Error) JSONObject() ErrorJSONMarshaller {
	extras := make([]ErrorJSONMarshaller, len(ne.extras))
	for i, e := range ne.extras {
		extras[i] = e.JSONObject()
	}
	return ErrorJSONMarshaller{
		Subject: ne.subject,
		Err:     ne.err.Error(),
		Extras:  extras,
	}
}

func (ne Error) JSON() []byte {
	b, err := json.MarshalIndent(ne.JSONObject(), "", "  ")
	if err != nil {
		panic(err)
	}
	return b
}

func (ne Error) NoError() bool {
	return ne == nil
}

func (ne Error) HasError() bool {
	return ne != nil
}

func errorf(format string, args ...any) Error {
	for i, arg := range args {
		if err, ok := arg.(error); ok {
			if unwrapped := errors.Unwrap(err); unwrapped != nil {
				args[i] = unwrapped
			}
		}
	}
	return From(fmt.Errorf(format, args...))
}

func fromJSONObject(obj ErrorJSONMarshaller) (Error, bool) {
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, false
	}
	return FromJSON(data)
}

func (ne Error) withError(err Error) Error {
	if ne != nil && err != nil {
		ne.extras = append(ne.extras, *err)
	}
	return ne
}

func (ne Error) appendMsg(msg string) Error {
	if ne == nil {
		return nil
	}
	ne.err = fmt.Errorf("%w %s", ne.err, msg)
	return ne
}

func (ne Error) writeToSB(sb *strings.Builder, level int, prefix string) {
	for range level {
		sb.WriteString("  ")
	}
	sb.WriteString(prefix)

	if ne.NoError() {
		sb.WriteString("nil")
		return
	}

	if ne.subject != "" {
		sb.WriteString(ne.subject)
		sb.WriteRune(' ')
	}
	sb.WriteString(ne.err.Error())
	if len(ne.extras) > 0 {
		sb.WriteRune(':')
		for _, extra := range ne.extras {
			sb.WriteRune('\n')
			extra.writeToSB(sb, level+1, "- ")
		}
	}
}

func (ne Error) buildError(level int, prefix string) error {
	var res error
	var sb strings.Builder

	for range level {
		sb.WriteString("  ")
	}
	sb.WriteString(prefix)

	if ne.NoError() {
		sb.WriteString("nil")
		return errors.New(sb.String())
	}

	res = fmt.Errorf("%s%w", sb.String(), ne.err)
	sb.Reset()

	if ne.subject != "" {
		sb.WriteString(fmt.Sprintf(" for %q", ne.subject))
	}
	if len(ne.extras) > 0 {
		sb.WriteRune(':')
		res = fmt.Errorf("%w%s", res, sb.String())
		for _, extra := range ne.extras {
			res = errors.Join(res, extra.buildError(level+1, "- "))
		}
	} else {
		res = fmt.Errorf("%w%s", res, sb.String())
	}
	return res
}
