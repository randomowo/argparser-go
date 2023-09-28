package main

import (
	"fmt"
)

type NotAPointerToStruct struct{}

func (NotAPointerToStruct) Error() string {
	return "passed value is not a pointer to a struct"
}

type InvalidField struct {
	Key string
}

func (e *InvalidField) Error() string {
	return fmt.Sprintf("invalid settings for %s field", e.Key)
}

type MissingMainTag struct {
	Key string
}

func (e *MissingMainTag) Error() string {
	return fmt.Sprintf("missing `argparser` tag for %s field", e.Key)
}

type EmptyTag struct {
	FieldName string
	TagName   string
}

func (e *EmptyTag) Error() string {
	return fmt.Sprintf("tag `%s` is empty for %s field", e.TagName, e.FieldName)
}

type UnknownMainTagType struct {
	FieldName string
	Type      string
}

func (e *UnknownMainTagType) Error() string {
	return fmt.Sprintf("field %s have unknown main tag type: %s", e.FieldName, e.Type)
}

type NotValidTag struct {
	FieldName string
	TagName   TagName
	ErrorDesc string
}

func (e *NotValidTag) Error() string {
	return fmt.Sprintf("field %s have not valid `%s` tag: %s", e.FieldName, e.TagName, e.ErrorDesc)
}

type HelpMessage struct {
	Message string
}

func (e *HelpMessage) Error() string {
	return e.Message
}

type NotEnoughRequiredArgs struct {
	Expected int
	Actual   int
}

func (e *NotEnoughRequiredArgs) Error() string {
	return fmt.Sprintf("not enougth args, expected: %d, actual: %d", e.Expected, e.Actual)
}

type ArgNotFound struct {
	ArgType ArgType
	Extra   string
}

func (e *ArgNotFound) Error() string {
	res := fmt.Sprintf("argument of type %s not passed to args", e.ArgType)
	if e.ArgType != Arg {
		res = fmt.Sprintf("%s: %s", res, e.Extra)
	}
	return res
}

type WrongArgValue struct {
	ErrorDesc string
}

func (e *WrongArgValue) Error() string {
	return fmt.Sprintf("wrong argument value: %s", e.ErrorDesc)
}

type WrongValueType struct {
	Expected string
	Actual   string
}

func (e *WrongValueType) Error() string {
	return fmt.Sprintf("value type differ from expected %s != %s", e.Expected, e.Actual)
}
