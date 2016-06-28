package medco_test

import (
	"reflect"
	"testing"
	"github.com/dedis/cothority/log"
	"github.com/dedis/cothority/sda"
	"github.com/dedis/cothority/services/medco"
	. "github.com/dedis/cothority/services/medco/libmedco"
)

//number of group attributes
const numberGrpAttr = 1
//number of attributes
const numberAttr = 10
func TestServiceMedco(t *testing.T) {
	defer log.AfterTest(t)
	log.TestOutput(testing.Verbose(), 1)
	local := sda.NewLocalTest()
	// generate 5 hosts, they don't connect, they process messages, and they
	// don't register the tree or entitylist
	_, el, _ := local.GenTree(5, true, true, true)
	defer local.CloseAll()

	// Send a request to the service
	client := medco.NewMedcoClient(el.List[0])

	surveyDesc := SurveyDescription{1, 10}
	surveyID, err := client.CreateSurvey(el, surveyDesc)
	if err != nil {
		t.Fatal("Service did not start.")
	}

	//save values in a map to verify them at the end
	expectedResults := make(map[[numberGrpAttr]int64][]int64)
	log.Lvl1("Sending response data... ")
	dataHolder := make([]*medco.MedcoAPI, 10)
	for i := 0; i < numberAttr; i++ {
		dataHolder[i] = medco.NewMedcoClient(el.List[i%5])
		grp := [numberGrpAttr]int64{}
		aggr := make([]int64, 10)
		grp[0] = int64(i % 4)
		aggr[i] = 3

		//convert tab in slice (was a tab only for the test)
		sliceGrp := make([]int64,numberGrpAttr)
		for i,v := range grp {
			if i == 0 {
				sliceGrp = []int64{v}
			} else {
				sliceGrp = append(sliceGrp, v)
			}
		}

		dataHolder[i].SendSurveyResultsData(*surveyID, sliceGrp, aggr, el.Aggregate)

		//compute expected results
		_,ok := expectedResults[grp]
		if ok {
			for ind,v := range expectedResults[grp]{
				expectedResults[grp][ind] = v + aggr[ind]
			}
		} else {
			expectedResults[grp] = aggr
		}
	}

	grp, aggr, err := client.GetSurveyResults(*surveyID)

	if err != nil {
		t.Fatal("Service could not output the results.")
	}

	log.Lvl1("Service output:")
	for i,_ := range *grp {
		log.Lvl1(i, ")", (*grp)[i], "->", (*aggr)[i])
		//convert from slice to tab in order to test the values
		grpTab := [numberGrpAttr]int64{}
		for ind,v := range (*grp)[i]{
			grpTab[ind] = v
		}
		data, ok := expectedResults[grpTab]
		if !ok || !reflect.DeepEqual(data,(*aggr)[i]){
			t.Error("Not expected results, got ", (*aggr)[i], " when expected ", data)
		}
	}
}
