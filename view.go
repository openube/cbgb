//  Copyright (c) 2013 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the
//  License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing,
//  software distributed under the License is distributed on an "AS
//  IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
//  express or implied. See the License for the specific language
//  governing permissions and limitations under the License.

package cbgb

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	"github.com/couchbaselabs/walrus"
)

type Form interface {
	FormValue(key string) string
}

// Originally from github.com/couchbaselabs/walrus, but using
// pointers to structs instead of just structs.

type ViewResult struct {
	TotalRows int      `json:"total_rows"`
	Rows      ViewRows `json:"rows"`
}

type ViewRows []*ViewRow

type ViewRow struct {
	Id    string      `json:"id,omitempty"`
	Key   interface{} `json:"key,omitempty"`
	Value interface{} `json:"value,omitempty"`
}

func (rows ViewRows) Len() int {
	return len(rows)
}

func (rows ViewRows) Swap(i, j int) {
	rows[i], rows[j] = rows[j], rows[i]
}

func (rows ViewRows) Less(i, j int) bool {
	return walrus.CollateJSON(rows[i].Key, rows[j].Key) < 0
}

// From http://wiki.apache.org/couchdb/HTTP_view_API
type ViewParams struct {
	Key           string      `json:"key"`
	Keys          string      `json:"keys"`
	StartKey      interface{} `json:"startkey"`
	StartKeyDocId string      `json:"startkey_docid"`
	EndKey        interface{} `json:"endkey"`
	EndKeyDocId   string      `json:"endkey_docid"`
	Stale         string      `json:"stale"`
	Descending    bool        `json:"descending"`
	Group         bool        `json:"group"`
	GroupLevel    uint64      `json:"group_level"`
	IncludeDocs   bool        `json:"include_docs"`
	InclusiveEnd  bool        `json:"inclusive_end"`
	Limit         uint64      `json:"limit"`
	Reduce        bool        `json:"reduce"`
	Skip          uint64      `json:"skip"`
	UpdateSeq     bool        `json:"update_seq"`
}

func NewViewParams() *ViewParams {
	return &ViewParams{
		Reduce:       true,
		InclusiveEnd: true,
	}
}

func paramFieldName(sf reflect.StructField) string {
	fieldName := sf.Tag.Get("json")
	if fieldName == "" {
		fieldName = sf.Name
	}
	return fieldName
}

func ParseViewParams(params Form) (p *ViewParams, err error) {
	p = NewViewParams()
	if params == nil {
		return p, nil
	}

	val := reflect.Indirect(reflect.ValueOf(p))

	for i := 0; i < val.NumField(); i++ {
		sf := val.Type().Field(i)
		paramName := paramFieldName(sf)
		paramVal := params.FormValue(paramName)

		switch {
		case paramVal == "":
			// Skip this one
		case sf.Type.Kind() == reflect.String:
			val.Field(i).SetString(paramVal)
		case sf.Type.Kind() == reflect.Uint64:
			v := uint64(0)
			v, err = strconv.ParseUint(paramVal, 10, 64)
			if err != nil {
				return nil, err
			}
			val.Field(i).SetUint(v)
		case sf.Type.Kind() == reflect.Bool:
			val.Field(i).SetBool(paramVal == "true")
		case sf.Type.Kind() == reflect.Interface:
			var ob interface{}
			err := json.Unmarshal([]byte(paramVal), &ob)
			if err != nil {
				return p, err
			}
			val.Field(i).Set(reflect.ValueOf(ob))
		default:
			return nil, fmt.Errorf("Unhandled type in field %v", sf.Name)
		}
	}

	return p, nil
}
