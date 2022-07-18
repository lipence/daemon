package daemon

import "log"

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
