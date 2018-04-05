package merge_values

import (
	"encoding/json"
	"github.com/flant/antiopa/utils"
	"github.com/evanphx/json-patch"
	"github.com/mohae/deepcopy"
	"reflect"
)

type mergeValuesPair struct {
	A map[interface{}]interface{}
	B map[interface{}]interface{}
}

func mergeTwoValues(A map[interface{}]interface{}, B map[interface{}]interface{}) map[interface{}]interface{} {
	res := make(map[interface{}]interface{})
	for key, value := range A {
		res[key] = value
	}

	queue := []mergeValuesPair{{A: res, B: B}}

	for len(queue) > 0 {
		pair := queue[0]
		queue = queue[1:]

		for k, v2 := range pair.B {
			v1, isExist := pair.A[k]

			if isExist {
				v1Type := reflect.TypeOf(v1)
				v2Type := reflect.TypeOf(v2)

				if v1Type == v2Type {
					switch v1Type.Kind() {
					case reflect.Map:
						resMap := make(map[interface{}]interface{})
						for key, value := range v1.(map[interface{}]interface{}) {
							resMap[key] = value
						}
						pair.A[k] = resMap

						queue = append(queue, mergeValuesPair{
							A: resMap,
							B: v2.(map[interface{}]interface{}),
						})
					case reflect.Array, reflect.Slice:
						resArr := make([]interface{}, 0)
						for _, elem := range v1.([]interface{}) {
							resArr = append(resArr, elem)
						}
						for _, elem := range v2.([]interface{}) {
							resArr = append(resArr, elem)
						}
						pair.A[k] = resArr
					default:
						pair.A[k] = v2
					}
				} else {
					pair.A[k] = v2
				}
			} else {
				pair.A[k] = v2
			}
		}
	}

	return res
}

func MergeValues(ValuesArr ...map[interface{}]interface{}) map[interface{}]interface{} {
	res := make(map[interface{}]interface{})

	for _, values := range ValuesArr {
		res = mergeTwoValues(res, values)
	}

	return res
}

func ApplyJsonMergeAndPatch(values map[interface{}]interface{}, jsonValuesToMerge map[string]interface{}, patch *jsonpatch.Patch) (map[interface{}]interface{}, bool, error) {
	var err error
	resValues := deepcopy.Copy(values).(map[interface{}]interface{})

	if jsonValuesToMerge != nil {
		resValues = MergeValues(resValues, utils.JsonValuesToValues(jsonValuesToMerge))
	}

	if patch != nil {
		if resValues, err = applyJsonPatch(resValues, patch); err != nil {
			return nil, false, err
		}
	}

	return resValues, !reflect.DeepEqual(values, resValues), nil
}

func applyJsonPatch(values map[interface{}]interface{}, patch *jsonpatch.Patch) (map[interface{}]interface{}, error) {
	jsonValues, err := utils.ValuesToJsonValues(values)
	if err != nil {
		return nil, err
	}

	jsonDoc, err := json.Marshal(jsonValues)
	if err != nil {
		return nil, err
	}

	resJsonDoc, err := patch.Apply(jsonDoc)
	if err != nil {
		return nil, err
	}

	resJsonValues := make(map[string]interface{})
	if err = json.Unmarshal(resJsonDoc, &resJsonValues); err != nil {
		return nil, err
	}

	resValues := utils.JsonValuesToValues(resJsonValues)

	return resValues, nil
}
