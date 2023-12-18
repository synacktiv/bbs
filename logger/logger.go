package logger

import (
	"io"
	"log"
)

type LogLevel byte

const (
	LogLevelQuiet LogLevel = iota
	LogLevelNormal
	LogLevelVerbose
)

type AuditLevel byte

const (
	AuditLevelNo AuditLevel = iota
	AuditLevelYes
)

type MetaLogger struct {
	logWriter   io.Writer
	auditWriter io.Writer

	_debug *log.Logger
	_audit *log.Logger
	_info  *log.Logger
	_error *log.Logger
	_fatal *log.Logger
	_panic *log.Logger
}

func NewMetaLogger(logWriter io.Writer, auditWriter io.Writer) *MetaLogger {
	l := MetaLogger{
		logWriter:   logWriter,
		auditWriter: auditWriter,
	}

	flags := log.LstdFlags
	l._debug = log.New(io.Discard, "[DEBUG] ", 0)
	l._audit = log.New(l.auditWriter, "[AUDIT] ", flags)
	l._info = log.New(l.logWriter, "[INFO] ", flags)
	l._error = log.New(l.logWriter, "[ERROR] ", flags)
	l._fatal = log.New(l.logWriter, "[FATAL] ", flags)
	l._panic = log.New(l.logWriter, "[PANIC] ", flags)

	return &l
}

func (l *MetaLogger) disableLogger(logger *log.Logger) {
	logger.SetOutput(io.Discard)
	logger.SetFlags(0)
}

func (l *MetaLogger) enableLogger(logger *log.Logger, flags int) {
	logger.SetOutput(l.logWriter)
	logger.SetFlags(flags)
}

func (l *MetaLogger) enableAudit(flags int) {
	l._audit.SetOutput(l.auditWriter)
	l._audit.SetFlags(flags)
}

func (l *MetaLogger) disableAudit() {
	l._audit.SetOutput(io.Discard)
	l._audit.SetFlags(0)
}

func (l *MetaLogger) Debug(v ...interface{}) {
	l._debug.Println(v...)
}

func (l *MetaLogger) Debugf(format string, v ...interface{}) {
	l._debug.Printf(format, v...)
}

func (l *MetaLogger) Info(v ...interface{}) {
	l._info.Println(v...)
}

func (l *MetaLogger) Infof(format string, v ...interface{}) {
	l._info.Printf(format, v...)
}

func (l *MetaLogger) Audit(v ...interface{}) {
	l._audit.Println(v...)
}

func (l *MetaLogger) Auditf(format string, v ...interface{}) {
	l._audit.Printf(format, v...)
}

func (l *MetaLogger) Error(v ...interface{}) {
	l._error.Println(v...)
}

func (l *MetaLogger) Errorf(format string, v ...interface{}) {
	l._error.Printf(format, v...)
}

func (l *MetaLogger) Fatal(v ...interface{}) {
	l._fatal.Fatal(v...)
}

func (l *MetaLogger) Fatalf(format string, v ...interface{}) {
	l._fatal.Fatalf(format, v...)
}

func (l *MetaLogger) Panic(v ...interface{}) {
	l._panic.Panic(v...)
}

func (l *MetaLogger) Panicf(format string, v ...interface{}) {
	l._panic.Panicf(format, v...)
}

func (l *MetaLogger) SetLogLevel(level LogLevel) {
	switch level {
	case LogLevelQuiet:
		l.disableLogger(l._debug)
		l.disableLogger(l._info)
		l.disableLogger(l._error)
		l.disableLogger(l._fatal)
		l.disableLogger(l._panic)
	case LogLevelNormal:
		l.disableLogger(l._debug)
		l.enableLogger(l._info, log.LstdFlags)
		l.enableLogger(l._error, log.LstdFlags)
		l.enableLogger(l._fatal, log.LstdFlags)
		l.enableLogger(l._panic, log.LstdFlags)
	case LogLevelVerbose:
		l.enableLogger(l._debug, log.LstdFlags)
		l.enableLogger(l._info, log.LstdFlags)
		l.enableLogger(l._error, log.LstdFlags)
		l.enableLogger(l._fatal, log.LstdFlags)
		l.enableLogger(l._panic, log.LstdFlags)
	}
}

func (l *MetaLogger) SetAuditLevel(level AuditLevel) {
	switch level {
	case AuditLevelYes:
		l.enableAudit(log.LstdFlags)
	case AuditLevelNo:
		l.disableAudit()
	}
}
