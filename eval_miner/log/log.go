package log

import (
	"fmt"
	"time"
)

var debugOn = false

func Errorf(format string, args ...interface{}) {
	fmt.Printf(time.Now().Format("2006-01-02 15:04:05")+": "+format+"\n", args...)
}

func Debugf(format string, args ...interface{}) {
	if debugOn {
		fmt.Printf(time.Now().Format("2006-01-02 15:04:05")+": "+format+"\n", args...)
	}
}

func Infof(format string, args ...interface{}) {
	fmt.Printf(time.Now().Format("2006-01-02 15:04:05")+": "+format+"\n", args...)
}

func Printf(format string, args ...interface{}) {
	fmt.Printf(time.Now().Format("2006-01-02 15:04:05")+": "+format+"\n", args...)
}

func Info(args ...interface{}) {
	fmt.Print(time.Now().Format("2006-01-02 15:04:05") + ": ")
	fmt.Print(args...)
	fmt.Print("\n")
}

func Error(args ...interface{}) {
	fmt.Print(args...)
	fmt.Print("\n")
}

func Debug(args ...interface{}) {
	if debugOn {
		fmt.Print(time.Now().Format("2006-01-02 15:04:05") + ": ")
		fmt.Print(args...)
		fmt.Print("\n")
	}
}
