package daemon

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

type errorBatch []error

func (b errorBatch) Error() string {
	var buf = strings.Builder{}
	buf.WriteString("Multiple error occurred:")
	for i, err := range b {
		buf.WriteString(fmt.Sprintf("\n[%d] %v", i, err))
	}
	return buf.String()
}

type errorCollection struct {
	rw         sync.RWMutex
	collection []error
}

func (c *errorCollection) append(e error) {
	c.rw.Lock()
	c.collection = append(c.collection, e)
	c.rw.Unlock()
}

func (c *errorCollection) batch() error {
	var errs []error
	c.rw.RLock()
	errs = c.collection
	c.rw.RUnlock()
	if len(errs) == 0 {
		return nil
	}
	var _errs = make([]error, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			_errs = append(_errs, err)
		}
	}
	switch len(_errs) {
	case 0:
		return nil
	case 1:
		return _errs[0]
	default:
		return errorBatch(_errs)
	}
}

const ErrTextUnspecifiedError = "(unspecified error)"

func anyAsErr(src any) error {
	if src == nil {
		return nil
	}
	switch _src := src.(type) {
	case error:
		return _src
	case fmt.Stringer:
		return errors.New(fallback(_src.String(), ErrTextUnspecifiedError))
	case string:
		return errors.New(fallback(_src, ErrTextUnspecifiedError))
	default:
		return errors.New(fallback(fmt.Sprintf("(%T) %v", src, src), ErrTextUnspecifiedError))
	}
}
