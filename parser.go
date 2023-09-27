package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

func Unmarshal(v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return NotAPointerToStruct{}
	}

	return unmarshal(rv.Elem())
}

type tagInfo struct {
	ArgType  ArgType
	Required bool
	Default  any
	Help     string
	Flags    []string
	Nargs    int
}

type field struct {
	Value        *reflect.Value
	Type         reflect.Type
	TagInfo      *tagInfo
	StructOffset int
}

type llArgs struct {
	Val  string
	Next *llArgs
	Prev *llArgs
}

func (ll *llArgs) Len() int {
	next := ll
	res := 0
	for next != nil {
		res++
		next = next.Next
	}
	return res
}

func (ll *llArgs) String() string {
	next := ll
	res := make([]string, ll.Len())
	for i := 0; i < len(res); i++ {
		res[i] = next.Val
		next = next.Next
	}

	return strings.Join(res, " ")
}

func newLLArgs(args []string) *llArgs {
	res := new(llArgs)
	next := res

	for index := 0; index < len(args); index++ {
		next.Next = new(llArgs)
		next.Next.Prev = next
		next = next.Next
		next.Val = args[index]
	}

	res.Next.Prev = nil
	return res.Next
}

func unmarshal(rv reflect.Value) error {
	var (
		err    error
		fields []*field
	)

	fields, err = parseFields(rv)
	if err != nil {
		return err
	}

	// move args to end
	for i := 0; i < len(fields); i++ {
		for j := 0; j < len(fields); j++ {
			if i == j {
				continue
			}

			if i < j && fields[i].TagInfo.ArgType == Arg {
				fields[i], fields[j] = fields[j], fields[i]
			}
		}
	}

	required := 0
	for _, f := range fields {
		if f.TagInfo.Required {
			required++
		}
	}

	args := newLLArgs(os.Args[1:])

	if required > args.Len() {
		return &NotEnoughRequiredArgs{
			Expected: required,
			Actual:   args.Len(),
		}
	}

	for index := 1; index < len(os.Args); index++ {
		if os.Args[index] == "--help" || os.Args[index] == "-h" {
			return &HelpMessage{
				Message: getHelpMessage(fields),
			}
		}
	}

	for _, f := range fields {
		f.Value = new(reflect.Value)
		if err = parseArg(f, args); err != nil {
			return err
		}

		rvField := rv.Field(f.StructOffset)
		rvField.Set(f.Value.Convert(rvField.Type()))
	}

	return nil
}

func parseArg(f *field, args *llArgs) error {
	next := args
	removeFromLL := func(current *llArgs) {
		if current.Prev == nil {
			current.Next.Prev = nil
			*args = *current.Next
		} else {
			*current.Prev.Next = *current.Next
			*current.Next.Prev = *current.Prev
		}
	}

	for next != nil {
		isFlag := strings.HasPrefix(next.Val, "-")
		prevIsFlag := false
		if next.Prev != nil {
			prevIsFlag = strings.HasPrefix(next.Prev.Val, "-")
		}

		switch {
		case f.TagInfo.ArgType == Arg && !isFlag && !prevIsFlag:
			val, err := parseStringValue(f.Type, next.Val)
			if err != nil {
				return &WrongArgValue{
					ErrorDesc: err.Error(),
				}
			}
			*f.Value = reflect.ValueOf(val)
			removeFromLL(next)
			return nil
		case f.TagInfo.ArgType == Option && isFlag && slices.Contains[[]string, string](f.TagInfo.Flags, next.Val):
			removeFromLL(next)

			if f.Type.Kind() != reflect.Array && f.Type.Kind() != reflect.Slice {
				val, err := parseStringValue(f.Type, next.Val)
				if err != nil {
					return &WrongArgValue{
						ErrorDesc: err.Error(),
					}
				}
				removeFromLL(next)

				*f.Value = reflect.ValueOf(val)
				return nil
			}

			additionalTest := func(str string) bool {
				if f.Type.Elem().Kind() != reflect.String {
					return !strings.HasPrefix(str, "-")
				}
				return true
			}

			vals := make([]string, 0)
			for next != nil && additionalTest(next.Val) && len(vals) < f.TagInfo.Nargs {
				vals = append(vals, next.Val)
				removeFromLL(next)
			}

			if len(vals) < f.TagInfo.Nargs {
				return &WrongArgValue{
					ErrorDesc: fmt.Sprintf("not enougth option values, required: %d, passed %d"),
				}
			}

			encoded, err := json.Marshal(vals)
			if err != nil {
				return &WrongArgValue{
					ErrorDesc: err.Error(),
				}
			}

			val, err := parseStringValue(f.Type, string(encoded))
			if err != nil {
				return &WrongArgValue{
					ErrorDesc: err.Error(),
				}
			}

			*f.Value = reflect.ValueOf(val)
			return nil

		case f.TagInfo.ArgType == Flag && isFlag && slices.Contains[[]string, string](f.TagInfo.Flags, next.Val):
			*f.Value = reflect.ValueOf(true)
			removeFromLL(next)
			return nil
		}

		next = next.Next
	}

	if f.TagInfo.Required {
		return &ArgNotFound{ArgType: f.TagInfo.ArgType}
	}

	if f.TagInfo.Default != nil {
		*f.Value = reflect.ValueOf(f.TagInfo.Default)
		return nil
	}

	*f.Value = reflect.Zero(f.Type)
	return nil
}

func getHelpMessage(fields []*field) string {
	res := make([]string, len(fields))
	for i, f := range fields {
		res[i] = f.TagInfo.Help
	}

	return strings.Join(res, "\n")
}

func parseFields(rv reflect.Value) ([]*field, error) {
	fields := make([]*field, len(reflect.VisibleFields(rv.Type())))

	for index, f := range reflect.VisibleFields(rv.Type()) {
		fv, err := processField(f, index)
		if err != nil {
			return nil, err
		}

		fields[index] = fv
	}

	return fields, nil
}

func processField(f reflect.StructField, offset int) (*field, error) {
	tf, err := processTags(f)
	if err != nil {
		return nil, err
	}

	rf := new(field)
	rf.Type = f.Type
	rf.StructOffset = offset
	rf.TagInfo = tf

	return rf, nil
}

type TagName string

const (
	mainTagName    TagName = "argparser"
	helpTagName            = mainTagName + "_help"
	defaultTagName         = mainTagName + "_default"
)

type ArgType string

const (
	Arg    = "arg"
	Option = "option"
	Flag   = "flag"
)

func processTags(f reflect.StructField) (*tagInfo, error) {
	info := new(tagInfo)

	mainTagValue, ok := f.Tag.Lookup(string(mainTagName))
	if !ok {
		return nil, &MissingMainTag{Key: f.Name}
	}
	if mainTagValue == "" {
		return nil, &EmptyTag{FieldName: f.Name, TagName: mainTagValue}
	}

	var tagType string
	params := strings.Split(mainTagValue, ",")
	switch params[0] {
	case Arg, Option, Flag:
		info.ArgType = ArgType(params[0])
	default:
		return nil, &UnknownMainTagType{FieldName: f.Name, Type: params[0]}
	}

	if info.ArgType == Flag && f.Type.Kind() != reflect.Bool {
		return nil, &NotValidTag{
			FieldName: f.Name,
			TagName:   mainTagName,
			ErrorDesc: "flag typed field must be bool",
		}
	}

	if info.ArgType != Arg && !strings.HasPrefix(params[1], "-") {
		return nil, &NotValidTag{
			FieldName: f.Name,
			TagName:   mainTagName,
			ErrorDesc: fmt.Sprintf("%s params must start with flags (like -name or --name)", tagType),
		}
	}

	if info.ArgType == Flag {
		info.Default = false
	}

	for index := 1; index < len(params); index++ {
		switch {
		case info.ArgType != Arg && strings.HasPrefix(params[index], "-"):
			info.Flags = append(info.Flags, params[index])

		case info.ArgType != Flag && params[index] == "required":
			info.Required = true

		case info.ArgType == Option && strings.HasPrefix(params[index], "nargs="):
			nargsValStr := params[index][len("nargs="):]
			nargs, err := strconv.Atoi(nargsValStr)
			if err != nil {
				return nil, &NotValidTag{
					FieldName: f.Name,
					TagName:   mainTagName,
					ErrorDesc: fmt.Sprintf("error while parsing nargs value: %s", nargsValStr),
				}
			}

			if nargs > 1 && f.Type.Kind() != reflect.Array && f.Type.Kind() != reflect.Slice {
				return nil, &NotValidTag{
					FieldName: f.Name,
					TagName:   mainTagName,
					ErrorDesc: fmt.Sprintf("not valid type for nargs>1: %v", f.Type),
				}
			}

			if f.Type.Kind() == reflect.Array && reflect.Zero(f.Type).Cap() < nargs {
				return nil, &NotValidTag{
					FieldName: f.Name,
					TagName:   mainTagName,
					ErrorDesc: "value ont nargs greater then capacity of field",
				}
			}

			info.Nargs = nargs

		default:
			return nil, &NotValidTag{
				FieldName: f.Name,
				TagName:   mainTagName,
				ErrorDesc: fmt.Sprintf("unknown param `%s` for %s tag type", params[index], tagType),
			}
		}
	}

	if helpTagValue, ok := f.Tag.Lookup(string(helpTagName)); ok {
		info.Help = helpTagValue
	}

	if defaultTagValue, ok := f.Tag.Lookup(string(defaultTagName)); ok {
		if info.ArgType == Arg {
			return nil, &NotValidTag{
				FieldName: f.Name,
				TagName:   defaultTagName,
				ErrorDesc: "arg typed field can not have default value",
			}
		}

		val, err := parseStringValue(f.Type, defaultTagValue)
		if err != nil {
			return nil, &NotValidTag{
				FieldName: f.Name,
				TagName:   defaultTagName,
				ErrorDesc: err.Error(),
			}
		}

		if _, ok := val.(bool); info.ArgType == Flag && !ok {
			return nil, &NotValidTag{
				FieldName: f.Name,
				TagName:   defaultTagName,
				ErrorDesc: "default value of flag typed field must be bool",
			}
		}

		info.Default = val
	}

	return info, nil
}

func parseStringValue(ft reflect.Type, s string) (any, error) {
	var parseFunc func(string) (any, error)

	switch ft.Kind() {
	case reflect.Struct, reflect.Slice, reflect.Array, reflect.Map:
		parseFunc = func(str string) (any, error) {
			res := reflect.Zero(ft).Interface()
			if err := json.Unmarshal([]byte(s), &res); err != nil {
				return nil, err
			}

			return res, nil
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parseFunc = func(str string) (any, error) {
			val, err := strconv.ParseInt(str, 10, 64)
			if err != nil {
				return nil, err
			}
			if reflect.Zero(ft).OverflowInt(val) {
				return nil, errors.New("int value bigger then field type")
			}
			return val, nil
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		parseFunc = func(str string) (any, error) {
			val, err := strconv.ParseUint(str, 10, 64)
			if err != nil {
				return nil, err
			}
			if reflect.Zero(ft).OverflowUint(val) {
				return nil, errors.New("uint value bigger then field type")
			}
			return val, nil
		}
	case reflect.Float32, reflect.Float64:
		parseFunc = func(str string) (any, error) {
			val, err := strconv.ParseFloat(str, 64)
			if err != nil {
				return nil, err
			}
			if reflect.Zero(ft).OverflowFloat(val) {
				return nil, errors.New("float value bigger then field type")
			}
			return val, nil
		}
	case reflect.Bool:
		parseFunc = func(str string) (any, error) {
			return strconv.ParseBool(str)
		}
	case reflect.String:
		parseFunc = func(str string) (any, error) {
			return str, nil
		}
	default:
		parseFunc = func(str string) (any, error) {
			return nil, errors.New(fmt.Sprintf("unsupported type %s of default value %s", ft, str))
		}
	}

	return parseFunc(s)
}
