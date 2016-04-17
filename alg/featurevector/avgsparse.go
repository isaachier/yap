package featurevector

import (
	"fmt"
	// "log"
	"sort"
	"strings"
	"sync"
	"yap/util"
)

type HistoryValue struct {
	sync.Mutex
	Generation     int
	PrevGeneration int
	Value, Total   int64
	IsSet          bool
}

func (h *HistoryValue) Integrate(generation int) {
	h.Value = h.IntegratedValue(generation)
}

func (h *HistoryValue) IntegratedValue(generation int) int64 {
	return h.Total + (int64)(generation-h.Generation)*h.Value
}
func (h *HistoryValue) Add(generation int, amount int64) {
	h.Lock()
	defer h.Unlock()
	if h.PrevGeneration < h.Generation {
		h.Total += (int64)(generation-h.Generation) * h.Value
	}
	if h.Generation < generation {
		h.PrevGeneration, h.Generation = h.Generation, generation
	}
	h.Value = h.Value + amount
}

func NewHistoryValue(generation int, value int64) *HistoryValue {
	return &HistoryValue{Generation: generation, Value: value}
}

type TransitionScoreKVFunc func(key int, value *HistoryValue)

type TransitionScoreStore interface {
	Add(generation, transition int, feature interface{}, amount int64)
	Integrate(generation int)
	Len() int
	SetValue(key int, value *HistoryValue)
	GetValue(key int) *HistoryValue
	Each(f TransitionScoreKVFunc)
	Init()
}

type LockedArray struct {
	sync.RWMutex
	// Vals []*HistoryValue
	// TODO: refactor to const
	arrayData [94]HistoryValue

	Data []HistoryValue
}

var _ TransitionScoreStore = &LockedArray{}

func (l *LockedArray) Init() {
	l.Data = l.arrayData[0:0]
}
func (l *LockedArray) ExtendFor(generation, transition int) {
	if transition < len(l.arrayData) {
		l.Data = l.arrayData[:transition+1]
	}
	newVals := make([]HistoryValue, transition+1)
	copy(newVals[0:len(l.Data)], l.Data[0:len(l.Data)])
	l.Data = newVals
}

func (l *LockedArray) Add(generation, transition int, feature interface{}, amount int64) {
	l.Lock()
	defer l.Unlock()
	if transition < len(l.Data) {
		// log.Println("\t\tAdding to existing array")
		if l.Data[transition].IsSet {
			// log.Println("\t\tAdding to existing history value")
			l.Data[transition].Add(generation, amount)
		} else {
			// log.Println("\t\tCreating new history value")
			l.Data[transition] = HistoryValue{Generation: generation, Value: amount, IsSet: true}
		}
		return
	} else {
		// log.Println("\t\tExtending array")
		l.ExtendFor(generation, transition)
		if transition >= len(l.Data) {
			panic("Despite extending, transition >= than Vals")
		}
		l.Data[transition] = HistoryValue{Generation: generation, Value: amount, IsSet: true}
		return
	}
}

func (l *LockedArray) SetValue(key int, value *HistoryValue) {
	// log.Println("\tSetting value for key", key)
	l.ExtendFor(value.Generation, key)
	l.Data[key] = *value
}

func (l *LockedArray) GetValue(key int) *HistoryValue {
	if key < len(l.Data) {
		// log.Println("\t\t\t\tGetting value for key", key)
		// log.Println("\t\t\t\tGot", l.Vals[key])
		return &l.Data[key]
	} else {
		// log.Println("\t\t\t\tKey longer than value array")
		return nil
	}
}

func (l *LockedArray) Integrate(generation int) {
	for _, v := range l.Data {
		if v.IsSet {
			v.Integrate(generation)
		}
	}
}

func (l *LockedArray) Len() int {
	return len(l.Data)
}

func (l *LockedArray) Each(f TransitionScoreKVFunc) {
	for i, hist := range l.Data {
		f(i, &hist)
	}
}

type LockedMap struct {
	sync.RWMutex
	Vals map[int]*HistoryValue
}

var _ TransitionScoreStore = &LockedMap{}

func (l *LockedMap) Init() {

}

func (l *LockedMap) Add(generation, transition int, feature interface{}, amount int64) {
	l.Lock()
	defer l.Unlock()

	if historyValue, ok := l.Vals[transition]; ok {
		historyValue.Add(generation, amount)
		return
	} else {
		l.Vals[transition] = NewHistoryValue(generation, amount)
		return
	}
}

func (l *LockedMap) Integrate(generation int) {
	for _, v := range l.Vals {
		v.Integrate(generation)
	}
}

func (l *LockedMap) Len() int {
	return len(l.Vals)
}

func (l *LockedMap) SetValue(key int, value *HistoryValue) {
	l.Vals[key] = value
}

func (l *LockedMap) GetValue(key int) *HistoryValue {
	if value, exists := l.Vals[key]; exists {
		return value
	} else {
		return nil
	}
}

func (l *LockedMap) Each(f TransitionScoreKVFunc) {
	for i, hist := range l.Vals {
		f(i, hist)
	}
}

type AvgSparse struct {
	sync.RWMutex
	Dense bool
	// Vals  map[Feature]TransitionScoreStore
	stores []TransitionScoreStore
	Vals   map[Feature]int
}

func (v *AvgSparse) Value(transition int, feature interface{}) int64 {
	offset, exists := v.Vals[feature]
	transitions := v.stores[offset]
	if exists && transition < transitions.Len() {
		if histValue := transitions.GetValue(transition); histValue != nil {
			return histValue.Value
		}
	}
	return 0.0
}

func (v *AvgSparse) Add(generation, transition int, feature interface{}, amount int64, wg *sync.WaitGroup) {
	v.Lock()
	defer v.Unlock()
	offset, exists := v.Vals[feature]
	if exists {
		// wg.Add(1)
		go func(w *sync.WaitGroup, i int) {
			transitions := v.stores[i]
			transitions.Add(generation, transition, feature, amount)
			w.Done()
		}(wg, offset)
	} else {
		var newTrans TransitionScoreStore
		if v.Dense {
			newTrans = &LockedArray{Data: make([]HistoryValue, transition+1)}
		} else {
			newTrans = &LockedMap{Vals: make(map[int]*HistoryValue, 5)}
		}
		newTrans.Init()
		newTrans.SetValue(transition, NewHistoryValue(generation, amount))
		if v.stores == nil {
			panic("Got nil stores")
		}
		v.Vals[feature] = len(v.stores)
		v.stores = append(v.stores, newTrans)
		wg.Done()
	}
}

func (v *AvgSparse) Integrate(generation int) *AvgSparse {
	for _, val := range v.stores {
		val.Integrate(generation)
	}
	return v
}

func (v *AvgSparse) SetScores(feature Feature, scores ScoredStore, integrated bool) {
	offset, exists := v.Vals[feature]
	if exists {
		// log.Println("\t\tSetting scores for feature", feature)
		// log.Println("\t\tAvg sparse", transitions)
		scores.IncAll(v.stores[offset], integrated)
		// log.Println("\t\tSetting scores for feature", feature)
		// log.Println("\t\t\t1. Exists")
		// transitionsLen := transitions.Len()
		// if cap(*scores) < transitionsLen { // log.Println("\t\t\t1.1 Scores array not large enough")
		// 	newscores := make([]int64, transitionsLen)
		// 	// log.Println("\t\t\t1.2 Copying")
		// 	copy(newscores[0:transitionsLen], (*scores)[0:len(*scores)])
		// 	// log.Println("\t\t\t1.3 Setting pointer")
		// 	*scores = newscores
		// }
		// log.Println("\t\t\t2. Iterating", len(transitions), "transitions")
		// transitions.Each(func(i int, val *HistoryValue) {
		// 	if val == nil {
		// 		return
		// 	}
		// 	// log.Println("\t\t\t\tAt transition", i)
		// 	// for len(*scores) <= i {
		// 	// 	// log.Println("\t\t\t\t2.2 extending scores of len", len(*scores), "up to", i)
		// 	// 	*scores = append(*scores, 0)
		// 	// }
		// 	// log.Println("\t\t\t\t2.3 incrementing with", val.Value)
		// 	// (*scores)[i] += val.Value
		//
		// 	scores.Inc(i, val.Value)
		// })
		// for i, val := range transitions.Values() {
		// 	if val == nil {
		// 		continue
		// 	}
		// 	// log.Println("\t\t\t\tAt transition", i)
		// 	for len(*scores) <= i {
		// 		// log.Println("\t\t\t\t2.2 extending scores of len", len(*scores), "up to", i)
		// 		*scores = append(*scores, 0)
		// 	}
		// 	// log.Println("\t\t\t\t2.3 incrementing with", val.Value)
		// 	(*scores)[i] += val.Value
		// }
		// log.Println("\t\tReturning scores array", *scores)
	}
}

func (v *AvgSparse) UpdateScalarDivide(byValue int64) *AvgSparse {
	if byValue == 0.0 {
		panic("Divide by 0")
	}
	v.RLock()
	defer v.RUnlock()
	for _, val := range v.stores {
		val.Each(func(i int, histValue *HistoryValue) {
			histValue.Value = histValue.Value / byValue
		})
	}
	return v
}

func (v *AvgSparse) String() string {
	strs := make([]string, 0, len(v.Vals))
	v.RLock()
	defer v.RUnlock()
	for feat, val := range v.Vals {
		strs = append(strs, fmt.Sprintf("%v %v", feat, val))
	}
	return strings.Join(strs, "\n")
}

func (v *AvgSparse) Serialize() interface{} {
	// retval := make(map[interface{}][]int64, len(v.Vals))
	retval := make(map[interface{}][]int64, len(v.Vals))
	for k, v := range v.stores {
		scores := make([]int64, v.Len())
		v.Each(func(i int, lastScore *HistoryValue) {
			if lastScore != nil {
				scores[i] = lastScore.Value
			}
		})
		// for i, lastScore := range v.Vals {
		// 	if lastScore != nil {
		// 		scores[i] = lastScore.Value
		// 	}
		// }
		retval[k] = scores
	}
	return retval
}

func (v *AvgSparse) Deserialize(serialized interface{}, generation int) {
	data, ok := serialized.(map[interface{}][]int64)
	if !ok {
		panic("Can't deserialize unknown serialization")
	}
	v.Vals = make(map[Feature]int, len(data))
	v.stores = make([]TransitionScoreStore, len(data))
	allKeys := make(util.ByGeneric, 0, len(data))
	for k, _ := range data {
		allKeys = append(allKeys, util.Generic{fmt.Sprintf("%v", k), k})
	}
	sort.Sort(allKeys)
	for _, k := range allKeys {
		datav := data[k.Value]
		// log.Println("\t\tKey", k.Key, "transitions", len(datav))
		// log.Println("\t\t\tValues", datav)
		scoreStore := v.newTransitionScoreStore(len(datav))
		for i, value := range datav {
			scoreStore.SetValue(i, NewHistoryValue(generation, value))
		}
		v.Vals[k.Value] = len(v.stores)
		v.stores = append(v.stores, scoreStore)
	}
}

func (v *AvgSparse) newTransitionScoreStore(size int) (store TransitionScoreStore) {
	if v.Dense {
		store = &LockedArray{Data: make([]HistoryValue, size)}
	} else {
		store = &LockedMap{Vals: make(map[int]*HistoryValue, size)}
	}
	return
}

func NewAvgSparse() *AvgSparse {
	return MakeAvgSparse(false)
}

func MakeAvgSparse(dense bool) *AvgSparse {
	return &AvgSparse{stores: make([]TransitionScoreStore, 0, 100), Vals: make(map[Feature]int, 100), Dense: dense}
}
