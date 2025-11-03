// SPDX-FileCopyrightText: 2024 NOI Techpark <digital@noi.bz.it>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package apigorowler

import (
	"context"
	"net/http"
	"testing"

	crawler_testing "github.com/noi-techpark/go-apigorowler/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExampleForeachValue(t *testing.T) {
	mockTransport := crawler_testing.NewMockRoundTripper(map[string]string{
		"https://www.onecenter.info/api/DAZ/FacilityFreePlaces?FacilityID=1": "testdata/crawler/example_foreach_value/facilities_1.json",
		"https://www.onecenter.info/api/DAZ/FacilityFreePlaces?FacilityID=2": "testdata/crawler/example_foreach_value/facilities_2.json",
	})

	craw, _, _ := NewApiCrawler("testdata/crawler/example_foreach_value.yaml")
	client := &http.Client{Transport: mockTransport}
	craw.SetClient(client)

	err := craw.Run(context.TODO())
	require.Nil(t, err)

	data := craw.GetData()

	var expected interface{}
	err = crawler_testing.LoadInputData(&expected, "testdata/crawler/example_foreach_value/output.json")
	require.Nil(t, err)

	assert.Equal(t, expected, data)
}

func TestExampleForeachValueCtx(t *testing.T) {
	mockTransport := crawler_testing.NewMockRoundTripper(map[string]string{
		"https://www.onecenter.info/api/DAZ/FacilityFreePlaces?FacilityID=1": "testdata/crawler/example_foreach_value_transform_ctx/facilities_1.json",
		"https://www.onecenter.info/api/DAZ/FacilityFreePlaces?FacilityID=2": "testdata/crawler/example_foreach_value_transform_ctx/facilities_2.json",
	})

	craw, _, _ := NewApiCrawler("testdata/crawler/example_foreach_value_transform_ctx.yaml")
	client := &http.Client{Transport: mockTransport}
	craw.SetClient(client)

	err := craw.Run(context.TODO())
	require.Nil(t, err)

	data := craw.GetData()

	var expected interface{}
	err = crawler_testing.LoadInputData(&expected, "testdata/crawler/example_foreach_value_transform_ctx/output.json")
	require.Nil(t, err)

	assert.Equal(t, expected, data)
}

func TestExampleForeachValueStream(t *testing.T) {
	mockTransport := crawler_testing.NewMockRoundTripper(map[string]string{
		"https://www.onecenter.info/api/DAZ/FacilityFreePlaces?FacilityID=1": "testdata/crawler/example_foreach_value/facilities_1.json",
		"https://www.onecenter.info/api/DAZ/FacilityFreePlaces?FacilityID=2": "testdata/crawler/example_foreach_value/facilities_2.json",
	})

	craw, _, _ := NewApiCrawler("testdata/crawler/example_foreach_value_stream.yaml")
	client := &http.Client{Transport: mockTransport}
	craw.SetClient(client)

	stream := craw.GetDataStream()
	defer close(stream)
	data := make([]interface{}, 0)

	go func() {
		for d := range stream {
			data = append(data, d)
		}
	}()

	err := craw.Run(context.TODO())
	require.Nil(t, err)

	var expected interface{}
	err = crawler_testing.LoadInputData(&expected, "testdata/crawler/example_foreach_value/output.json")
	require.Nil(t, err)

	assert.Equal(t, expected, data)
}

func TestExampleSingle(t *testing.T) {
	mockTransport := crawler_testing.NewMockRoundTripper(map[string]string{
		"https://www.onecenter.info/api/DAZ/GetFacilities":                   "testdata/crawler/example_single/facilities_1.json",
		"https://www.onecenter.info/api/DAZ/FacilityFreePlaces?FacilityID=2": "testdata/crawler/example_single/facility_id_2.json",
	})

	craw, _, _ := NewApiCrawler("testdata/crawler/example_single.yaml")
	client := &http.Client{Transport: mockTransport}
	craw.SetClient(client)

	err := craw.Run(context.TODO())
	require.Nil(t, err)

	data := craw.GetData()

	var expected interface{}
	err = crawler_testing.LoadInputData(&expected, "testdata/crawler/example_single/output.json")
	require.Nil(t, err)

	assert.Equal(t, expected, data)
}

func TestExample2(t *testing.T) {
	mockTransport := crawler_testing.NewMockRoundTripper(map[string]string{
		"https://www.onecenter.info/api/DAZ/GetFacilities":                    "testdata/crawler/example2/facilities_1.json",
		"https://www.onecenter.info/api/DAZ/FacilityFreePlaces?FacilityID=2":  "testdata/crawler/example2/facility_id_2.json",
		"https://www.onecenter.info/api/DAZ/FacilityFreePlaces?FacilityID=s3": "testdata/crawler/example2/facility_id_s3.json",
		"https://www.onecenter.info/api/DAZ/FacilityFreePlaces?FacilityID=s4": "testdata/crawler/example2/facility_id_s4.json",
		"https://www.onecenter.info/api/DAZ/Locations/l1":                     "testdata/crawler/example2/location_id_l1.json",
		"https://www.onecenter.info/api/DAZ/Locations/l2":                     "testdata/crawler/example2/location_id_l2.json",
		"https://www.onecenter.info/api/DAZ/Locations/l3":                     "testdata/crawler/example2/location_id_l3.json",
	})

	craw, _, _ := NewApiCrawler("testdata/crawler/example2.yaml")
	client := &http.Client{Transport: mockTransport}
	craw.SetClient(client)

	err := craw.Run(context.TODO())
	require.Nil(t, err)

	data := craw.GetData()

	var expected interface{}
	err = crawler_testing.LoadInputData(&expected, "testdata/crawler/example2/output.json")
	require.Nil(t, err)

	assert.Equal(t, expected, data)
}

func TestPaginatedIncrement(t *testing.T) {
	mockTransport := crawler_testing.NewMockRoundTripper(map[string]string{
		"https://www.onecenter.info/api/DAZ/GetFacilities?offset=0": "testdata/crawler/paginated_increment/facilities_1.json",
		"https://www.onecenter.info/api/DAZ/GetFacilities?offset=1": "testdata/crawler/paginated_increment/facilities_2.json",
	})

	craw, _, _ := NewApiCrawler("testdata/crawler/example_pagination_increment.yaml")
	client := &http.Client{Transport: mockTransport}
	craw.SetClient(client)

	err := craw.Run(context.TODO())
	require.Nil(t, err)

	data := craw.GetData()

	var expected interface{}
	err = crawler_testing.LoadInputData(&expected, "testdata/crawler/paginated_increment/output.json")
	require.Nil(t, err)

	assert.Equal(t, expected, data)
}

func TestPaginatedIncrementNested(t *testing.T) {
	mockTransport := crawler_testing.NewMockRoundTripper(map[string]string{
		"https://www.onecenter.info/api/DAZ/GetFacilities?offset=0":          "testdata/crawler/paginated_increment_stream/facilities_1.json",
		"https://www.onecenter.info/api/DAZ/GetFacilities?offset=1":          "testdata/crawler/paginated_increment_stream/facilities_2.json",
		"https://www.onecenter.info/api/DAZ/FacilityFreePlaces?FacilityID=1": "testdata/crawler/paginated_increment_stream/facility_id_1.json",
		"https://www.onecenter.info/api/DAZ/FacilityFreePlaces?FacilityID=2": "testdata/crawler/paginated_increment_stream/facility_id_2.json",
		"https://www.onecenter.info/api/DAZ/FacilityFreePlaces?FacilityID=3": "testdata/crawler/paginated_increment_stream/facility_id_3.json",
		"https://www.onecenter.info/api/DAZ/FacilityFreePlaces?FacilityID=4": "testdata/crawler/paginated_increment_stream/facility_id_4.json",
	})

	craw, _, _ := NewApiCrawler("testdata/crawler/example_pagination_increment_nested.yaml")
	client := &http.Client{Transport: mockTransport}
	craw.SetClient(client)

	err := craw.Run(context.TODO())
	require.Nil(t, err)

	data := craw.GetData()

	var expected interface{}
	err = crawler_testing.LoadInputData(&expected, "testdata/crawler/paginated_increment_stream/output.json")
	require.Nil(t, err)

	assert.Equal(t, expected, data)
}

func TestPaginatedIncrementStream(t *testing.T) {
	mockTransport := crawler_testing.NewMockRoundTripper(map[string]string{
		"https://www.onecenter.info/api/DAZ/GetFacilities?offset=0":          "testdata/crawler/paginated_increment_stream/facilities_1.json",
		"https://www.onecenter.info/api/DAZ/GetFacilities?offset=1":          "testdata/crawler/paginated_increment_stream/facilities_2.json",
		"https://www.onecenter.info/api/DAZ/FacilityFreePlaces?FacilityID=1": "testdata/crawler/paginated_increment_stream/facility_id_1.json",
		"https://www.onecenter.info/api/DAZ/FacilityFreePlaces?FacilityID=2": "testdata/crawler/paginated_increment_stream/facility_id_2.json",
		"https://www.onecenter.info/api/DAZ/FacilityFreePlaces?FacilityID=3": "testdata/crawler/paginated_increment_stream/facility_id_3.json",
		"https://www.onecenter.info/api/DAZ/FacilityFreePlaces?FacilityID=4": "testdata/crawler/paginated_increment_stream/facility_id_4.json",
	})

	craw, _, _ := NewApiCrawler("testdata/crawler/example_pagination_increment_stream.yaml")
	client := &http.Client{Transport: mockTransport}
	craw.SetClient(client)

	stream := craw.GetDataStream()
	defer close(stream)
	data := make([]interface{}, 0)

	go func() {
		for d := range stream {
			data = append(data, d)
		}
	}()

	err := craw.Run(context.TODO())
	require.Nil(t, err)

	var expected interface{}
	err = crawler_testing.LoadInputData(&expected, "testdata/crawler/paginated_increment_stream/output.json")
	require.Nil(t, err)

	assert.Equal(t, expected, data)
}

func TestPaginatedNextUrl(t *testing.T) {
	mockTransport := crawler_testing.NewMockRoundTripper(map[string]string{
		"https://www.onecenter.info/api/DAZ/GetFacilities": "testdata/crawler/next_url/facilities_1.json",
		"http://list.com/page2":                            "testdata/crawler/next_url/facilities_2.json",
	})

	craw, _, _ := NewApiCrawler("testdata/crawler/example_pagination_next.yaml")
	client := &http.Client{Transport: mockTransport}
	craw.SetClient(client)

	err := craw.Run(context.TODO())
	require.Nil(t, err)

	data := craw.GetData()

	var expected interface{}
	err = crawler_testing.LoadInputData(&expected, "testdata/crawler/next_url/output.json")
	require.Nil(t, err)

	assert.Equal(t, expected, data)
}

func TestParallelSimple(t *testing.T) {
	mockTransport := crawler_testing.NewMockRoundTripper(map[string]string{
		"https://api.example.com/items/1": "testdata/crawler/parallel/item_1.json",
		"https://api.example.com/items/2": "testdata/crawler/parallel/item_2.json",
		"https://api.example.com/items/3": "testdata/crawler/parallel/item_3.json",
		"https://api.example.com/items/4": "testdata/crawler/parallel/item_4.json",
		"https://api.example.com/items/5": "testdata/crawler/parallel/item_5.json",
	})

	craw, _, _ := NewApiCrawler("testdata/crawler/parallel/simple.yaml")
	client := &http.Client{Transport: mockTransport}
	craw.SetClient(client)

	err := craw.Run(context.TODO())
	require.Nil(t, err)

	data := craw.GetData()

	var expected interface{}
	err = crawler_testing.LoadInputData(&expected, "testdata/crawler/parallel/simple_output.json")
	require.Nil(t, err)

	assert.Equal(t, expected, data)
}

func TestParallelRateLimited(t *testing.T) {
	mockTransport := crawler_testing.NewMockRoundTripper(map[string]string{
		"https://api.example.com/items/1": "testdata/crawler/parallel/item_1.json",
		"https://api.example.com/items/2": "testdata/crawler/parallel/item_2.json",
		"https://api.example.com/items/3": "testdata/crawler/parallel/item_3.json",
	})

	craw, _, _ := NewApiCrawler("testdata/crawler/parallel/ratelimited.yaml")
	client := &http.Client{Transport: mockTransport}
	craw.SetClient(client)

	err := craw.Run(context.TODO())
	require.Nil(t, err)

	data := craw.GetData()

	var expected interface{}
	err = crawler_testing.LoadInputData(&expected, "testdata/crawler/parallel/ratelimited_output.json")
	require.Nil(t, err)

	assert.Equal(t, expected, data)
}

func TestParallelNoopMerge(t *testing.T) {
	mockTransport := crawler_testing.NewMockRoundTripper(map[string]string{
		"https://api.example.com/items/1": "testdata/crawler/parallel/item_1.json",
		"https://api.example.com/items/2": "testdata/crawler/parallel/item_2.json",
		"https://api.example.com/items/3": "testdata/crawler/parallel/item_3.json",
	})

	craw, _, _ := NewApiCrawler("testdata/crawler/parallel/noop_merge.yaml")
	client := &http.Client{Transport: mockTransport}
	craw.SetClient(client)

	err := craw.Run(context.TODO())
	require.Nil(t, err)

	data := craw.GetData()

	// Parallel execution doesn't guarantee order, so check for set equality
	resultMap, ok := data.(map[string]interface{})
	require.True(t, ok, "Result should be a map")

	items, ok := resultMap["items"].([]interface{})
	require.True(t, ok, "items should be an array")
	require.Len(t, items, 3, "Should have 3 items")

	// Check that all expected IDs are present
	ids := make(map[float64]bool)
	for _, item := range items {
		itemMap := item.(map[string]interface{})
		id := itemMap["id"].(float64)
		ids[id] = true
	}

	assert.True(t, ids[1], "Should contain item with id 1")
	assert.True(t, ids[2], "Should contain item with id 2")
	assert.True(t, ids[3], "Should contain item with id 3")
}
