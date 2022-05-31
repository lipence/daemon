package daemon

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/samber/lo"
)

type logger interface {
	Infof(format string, v ...interface{})
	Warnf(format string, v ...interface{})
	Errorf(format string, v ...interface{})
}

type defaultLoggerType struct {
	*log.Logger
}

func (l *defaultLoggerType) Infof(format string, v ...interface{}) {
	l.Printf("[daemon][info] "+format, v...)
}

func (l *defaultLoggerType) Warnf(format string, v ...interface{}) {
	l.Printf("[daemon][warn] "+format, v...)
}

func (l *defaultLoggerType) Errorf(format string, v ...interface{}) {
	l.Printf("[daemon][error] "+format, v...)
}

var defaultLogger = &defaultLoggerType{
	Logger: log.Default(),
}

type errorBatch []error

func (b errorBatch) Error() string {
	var buf = strings.Builder{}
	buf.WriteString("Multiple error occurred:")
	for i, err := range b {
		buf.WriteString(fmt.Sprintf("\n[%d] %v", i, err))
	}
	return buf.String()
}

type errorBatchFunc func([]error) error

func defaultErrorBatch(errs []error) error {
	var _errs = make([]error, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			_errs = append(_errs, err)
		}
	}
	if len(_errs) == 0 {
		return nil
	}
	return errorBatch(_errs)
}

func defaultValue[T comparable](src T, defaultVal T) T {
	if src == lo.Empty[T]() {
		return defaultVal
	}
	return src
}

const UnspecifiedErrText = "(unspecified)"

func anyAsErr(src any) error {
	if src == nil {
		return nil
	}
	switch _src := src.(type) {
	case error:
		return _src
	case fmt.Stringer:
		return errors.New(defaultValue(_src.String(), UnspecifiedErrText))
	case string:
		return errors.New(defaultValue(_src, UnspecifiedErrText))
	default:
		return errors.New(defaultValue(fmt.Sprintf("(%T) %v", src, src), UnspecifiedErrText))
	}
}
