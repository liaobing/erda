// Copyright (c) 2021 Terminus, Inc.
//
// This program is free software: you can use, redistribute, and/or modify
// it under the terms of the GNU Affero General Public License, version 3
// or later ("AGPL"), as published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful, but WITHOUT
// ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
// FITNESS FOR A PARTICULAR PURPOSE.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <http://www.gnu.org/licenses/>.

package orgapis

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/olivere/elastic"

	"github.com/erda-project/erda-infra/modcom/api"
	"github.com/erda-project/erda-infra/providers/httpserver"
	"github.com/erda-project/erda-infra/providers/i18n"
	queryv1 "github.com/erda-project/erda/modules/core/monitor/metric/query/query/v1"
	"github.com/erda-project/erda/modules/monitor/utils"
)

func (p *provider) getContainers(ctx httpserver.Context, r *http.Request, params struct {
	InstanceType string `param:"instance_type" validate:"required"`
	Start        int64  `query:"start"`
	End          int64  `query:"end"`
}, res resourceRequest) interface{} {
	err := p.checkOrgByClusters(ctx, res.Clusters)
	if err != nil {
		return nil
	}
	now, timeRange := time.Now().UnixNano()/int64(time.Millisecond), 5*int64(time.Minute)/int64(time.Millisecond)
	if params.End < timeRange {
		params.End = now
	}
	if params.Start <= 0 {
		params.Start = params.End - timeRange
	}

	var (
		wg     sync.WaitGroup
		lock   sync.RWMutex
		result = make([]*containerData, 0, 16*len(res.Clusters))
	)
	wg.Add(len(res.Clusters))
	for _, cluster := range res.Clusters {
		go func(clusterName string, hostIPs []string) {
			defer wg.Done()
			containers := p.queryContainers(clusterName, hostIPs, params.InstanceType, res.Filters, params.Start, params.End)
			lock.Lock()
			defer lock.Unlock()
			result = append(result, containers...)
		}(cluster.ClusterName, cluster.HostIPs)
	}
	wg.Wait()
	return api.Success(result)
}

func (p *provider) queryContainers(cluster string, hostIPs []string, instanceType string, filters []*resourceFilter, start, end int64) []*containerData {
	query := elastic.NewBoolQuery().
		Filter(elastic.NewTermQuery(tagsClusterName, cluster)).
		Filter(elastic.NewRangeQuery(timestamp).Gte(start * int64(time.Millisecond)).Lt(end * int64(time.Millisecond))).
		Filter(elastic.NewTermsQuery(tagsHostIP, utils.ConvertStringArrToInterfaceArr(hostIPs)...))
	if instanceType != instanceTypeAll {
		query = query.Filter(elastic.NewTermQuery(tagsInstanceType, instanceType))
	}
	for _, filter := range filters {
		key := convertKey(filter.Key)
		if key == fieldsLabels {
			for _, v := range filter.Values {
				query.Filter(elastic.NewTermQuery(fieldsLabels, v))
			}
		} else {
			query.Filter(elastic.NewTermsQuery(key, utils.ConvertStringArrToInterfaceArr(filter.Values)...))
		}
	}

	topHitsAgg := elastic.NewTopHitsAggregation().Size(1).Sort(timestamp, false).
		FetchSourceContext(elastic.NewFetchSourceContext(true).Include(any))
	containerIDAgg := elastic.NewTermsAggregation().Field(tagsContainerID).Size(500).
		SubAggregation(tagsTerminusVersion, elastic.NewTermsAggregation().Field(tagsTerminusVersion).Size(100).
			SubAggregation(topHits, topHitsAgg)).
		SubAggregation(topHits, topHitsAgg)
	searchSource := elastic.NewSearchSource().Query(query).Size(0).Aggregation(tagsContainerID, containerIDAgg)

	indices := p.metricq.Indices([]string{nameContainerSummary}, []string{cluster}, start, end)
	resp, apiErr := p.metricq.SearchRaw(indices, searchSource)
	if apiErr != nil {
		return nil
	} else if resp == nil {
		return nil
	}
	return parseQueryContainer(resp.Aggregations)
}

func parseQueryContainer(agg elastic.Aggregations) []*containerData {
	if agg == nil {
		return nil
	}
	containerIDAgg, ok := agg.Terms(tagsContainerID)
	if !ok {
		return nil
	}
	var containersData []*containerData
	for _, containerIDItem := range containerIDAgg.Buckets {
		topHitsAgg, _ := containerIDItem.TopHits(topHits)
		if versionAgg, ok := containerIDItem.Terms(tagsTerminusVersion); ok {
			var maxVersion int64
			for _, versionItem := range versionAgg.Buckets {
				if version, err := versionItem.KeyNumber.Int64(); err != nil {
					continue
				} else if version > maxVersion {
					topHitsAgg, ok = versionItem.Aggregations.TopHits(topHits)
					if !ok {
						continue
					}
					maxVersion = version
				}
			}
		}
		data := wrapContainerData(topHitsAgg)
		containersData = append(containersData, data...)
	}
	return containersData
}

func wrapContainerData(topHits *elastic.AggregationTopHitsMetric) []*containerData {
	if topHits == nil || topHits.Hits == nil {
		return nil
	}

	var containersData []*containerData
	for _, hit := range topHits.Hits.Hits {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(*hit.Source), &m); err != nil {
			continue
		}

		tags, ok := utils.GetMapValueMap(m, tags)
		if !ok {
			continue
		}
		fields, ok := utils.GetMapValueMap(m, fields)
		if !ok {
			continue
		}

		isDeleted, _ := utils.GetMapValueString(tags, isDeleted)
		if isDeleted == "true" {
			continue
		}
		if _, ok := utils.GetMapValueString(fields, image); !ok {
			continue
		}

		containerData := new(containerData)
		containerData.ClusterName, _ = utils.GetMapValueString(tags, clusterName)
		containerData.HostIP, _ = utils.GetMapValueString(tags, hostIP)
		containerData.ContainerID, _ = utils.GetMapValueString(tags, containerID)
		containerData.InstanceType, _ = utils.GetMapValueString(tags, instanceType)
		containerData.InstanceID, _ = utils.GetMapValueString(tags, instanceID)
		containerData.Image, _ = utils.GetMapValueString(fields, image)
		containerData.OrgID, _ = utils.GetMapValueString(tags, orgID)
		containerData.OrgName, _ = utils.GetMapValueString(tags, orgName)
		containerData.ProjectID, _ = utils.GetMapValueString(tags, projectID)
		containerData.ProjectName, _ = utils.GetMapValueString(tags, projectName)
		containerData.ApplicationID, _ = utils.GetMapValueString(tags, applicationID)
		containerData.ApplicationName, _ = utils.GetMapValueString(tags, applicationName)
		containerData.Workspace, _ = utils.GetMapValueString(tags, workspace)
		containerData.RuntimeID, _ = utils.GetMapValueString(tags, runtimeID)
		containerData.RuntimeName, _ = utils.GetMapValueString(tags, runtimeName)
		containerData.ServiceID, _ = utils.GetMapValueString(tags, serviceID)
		containerData.ServiceName, _ = utils.GetMapValueString(tags, serviceName)
		containerData.JobID, _ = utils.GetMapValueString(tags, jobID)
		containerData.CpuUsage, _ = utils.GetMapValueFloat64(fields, cpuUsage)
		containerData.CpuRequest, _ = utils.GetMapValueFloat64(fields, cpuRequest)
		containerData.CpuLimit, _ = utils.GetMapValueFloat64(fields, cpuLimit)
		containerData.CpuOrigin, _ = utils.GetMapValueFloat64(fields, cpuOrigin)
		containerData.MemUsage, _ = utils.GetMapValueFloat64(fields, memUsage)
		containerData.MemRequest, _ = utils.GetMapValueFloat64(fields, memRequest)
		containerData.MemLimit, _ = utils.GetMapValueFloat64(fields, memLimit)
		containerData.MemOrigin, _ = utils.GetMapValueFloat64(fields, memOrigin)
		containerData.DiskUsage, _ = utils.GetMapValueFloat64(fields, diskUsage)
		containerData.DiskLimit, _ = utils.GetMapValueFloat64(fields, diskLimit)

		containersData = append(containersData, containerData)
	}
	return containersData
}

func (p *provider) groupContainerAllocation(ctx httpserver.Context, params struct {
	MetricType string `param:"metric_type" validate:"required"`
	Start      int64  `query:"start"`
	End        int64  `query:"end"`
	Limit      int    `query:"limit"`
	OrgName    string `query:"orgName"`
}, res resourceRequest) interface{} {
	err := p.checkOrgByClusters(ctx, res.Clusters)
	if err != nil {
		return nil
	}
	now, timeRange := time.Now().UnixNano()/int64(time.Millisecond), 5*int64(time.Minute)/int64(time.Millisecond)
	if params.End < timeRange {
		params.End = now
	}
	if params.Start <= 0 {
		params.Start = params.End - timeRange
	}
	if params.Limit <= 0 {
		params.Limit = 4
	}

	var (
		lang   = api.Language(ctx.Request())
		wg     sync.WaitGroup
		lock   sync.RWMutex
		result = make([]*resourceChart, 0, 16*len(res.Clusters))
	)
	wg.Add(len(res.Clusters))
	for _, cluster := range res.Clusters {
		go func(clusterName string, hostIPs []string) {
			defer wg.Done()
			chart := p.getContainerGroupAlloc(params.OrgName, clusterName, hostIPs, params.MetricType, params.Start, params.End, params.Limit, lang)
			lock.Lock()
			defer lock.Unlock()
			result = append(result, chart)
		}(cluster.ClusterName, cluster.HostIPs)
	}
	wg.Wait()

	resp := p.mergeResourceChart(result)
	return api.Success(resp)
}

func (p *provider) getContainerGroupAlloc(orgName, cluster string, hostIPs []string, metricType string, start, end int64, limit int, lang i18n.LanguageCodes) *resourceChart {
	var hostIPFilter string
	for _, hostIP := range hostIPs {
		hostIPFilter += "&in_host_ip=" + hostIP
	}
	resp, err := p.metricq.QueryWithFormatV1("params", "ajs_alloc/histogram?"+
		"start="+strconv.FormatInt(start, 10)+
		"&end="+strconv.FormatInt(end, 10)+
		"&filter_cluster_name="+cluster+
		"&filter_org_name="+orgName+
		hostIPFilter+
		"&group_reduce="+url.QueryEscape("{group=tags."+addonID+"&avg=fields."+metricType+"_allocation&reduce=sum}")+
		"&group_reduce="+url.QueryEscape("{group=tags."+serviceID+"&avg=fields."+metricType+"_allocation&reduce=sum}")+
		"&group_reduce="+url.QueryEscape("{group=tags."+jobID+"&avg=fields."+metricType+"_allocation&reduce=sum}")+
		"&in_instance_type=addon&in_instance_type=service&in_instance_type=job", "chart", lang)
	if err != nil {
		return nil
	}
	return p.parseContainerGroup(resp)
}

func (p *provider) groupContainerCount(ctx httpserver.Context, params struct {
	Start   int64  `query:"start"`
	End     int64  `query:"end"`
	Limit   int    `query:"limit"`
	OrgName string `query:"orgName"`
}, res resourceRequest) interface{} {
	err := p.checkOrgByClusters(ctx, res.Clusters)
	if err != nil {
		return nil
	}
	now, timeRange := time.Now().UnixNano()/int64(time.Millisecond), 5*int64(time.Minute)/int64(time.Millisecond)
	if params.End < timeRange {
		params.End = now
	}
	if params.Start <= 0 {
		params.Start = params.End - timeRange
	}
	if params.Limit <= 0 {
		params.Limit = 4
	}

	var (
		lang   = api.Language(ctx.Request())
		wg     sync.WaitGroup
		lock   sync.RWMutex
		result = make([]*resourceChart, 0, 16*len(res.Clusters))
	)
	wg.Add(len(res.Clusters))
	for _, cluster := range res.Clusters {
		go func(clusterName string, hostIPs []string) {
			defer wg.Done()
			chart := p.getContainerGroupCount(params.OrgName, clusterName, hostIPs, params.Start, params.End, params.Limit, lang)
			lock.Lock()
			defer lock.Unlock()
			result = append(result, chart)
		}(cluster.ClusterName, cluster.HostIPs)
	}
	wg.Wait()

	resp := p.mergeResourceChart(result)
	return api.Success(resp)
}

func (p *provider) getContainerGroupCount(orgName, cluster string, hostIPs []string, start, end int64, limit int, lang i18n.LanguageCodes) *resourceChart {
	var hostIPFilter string
	for _, hostIP := range hostIPs {
		hostIPFilter += "&in_host_ip=" + hostIP
	}
	resp, err := p.metricq.QueryWithFormatV1("params", "ajs_count/histogram?"+
		"start="+strconv.FormatInt(start, 10)+
		"&end="+strconv.FormatInt(end, 10)+
		"&filter_cluster_name="+cluster+
		hostIPFilter+
		"filter_org_name="+orgName+
		"&cardinality="+tagsAddonID+
		"&cardinality="+tagsServiceID+
		"&cardinality="+tagsJobID+
		"&in_instance_type=addon&in_instance_type=service&in_instance_type=job",
		"chart", lang)
	if err != nil {
		return nil
	}
	return p.parseContainerGroup(resp)
}

func (p *provider) parseContainerGroup(resp *queryv1.Response) *resourceChart {
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		return nil
	}
	val, ok := data["data"]
	if !ok {
		return nil
	}

	item, ok := val.(map[string]interface{})
	if !ok {
		return nil
	}
	t, ok := data["times"]
	if !ok {
		return nil
	}
	reduce := make([]map[string]*resourceChartData, 0)
	for k, v := range item {
		jsonStr, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		var chart resourceChartData
		err = json.Unmarshal(jsonStr, &chart)
		if err != nil {
			return nil
		}
		reduce = append(reduce, map[string]*resourceChartData{
			k: &chart,
		})
	}

	chart := &resourceChart{
		Total: resp.Total,
		Results: []*resourceChartResult{
			{
				Name: resp.Request().Name,
				Data: reduce,
			},
		},
	}

	chart.Title, _ = utils.GetMapValueString(data, "title")
	chart.Time, _ = t.([]int64)
	return chart
}

func (p *provider) mergeResourceChart(list []*resourceChart) *resourceChart {
	var res *resourceChart
	for _, item := range list {
		if res == nil || len(res.Results) == 0 || len(res.Results[0].Data) == 0 {
			res = item
			continue
		}
		if len(item.Results) == 0 || len(item.Results[0].Data) == 0 {
			continue
		}
		resData := res.Results[0].Data[0]
		itemData := item.Results[0].Data[0]
		for k, v := range itemData {
			val, ok := resData[k]
			if !ok {
				resData[k] = v
				continue
			}
			for i, item := range v.Data {
				if len(v.Data) > i {
					val.Data[i] += item
				} else {
					val.Data = append(val.Data, item)
				}
			}
		}
	}
	return res
}
