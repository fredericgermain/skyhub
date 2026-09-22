package main

import (
	"encoding/json"
)

// haDevice is the shared Home Assistant device block.
type haDevice struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model"`
	SWVersion    string   `json:"sw_version,omitempty"`
}

type haEntity struct {
	Name                string   `json:"name"`
	UniqueID            string   `json:"unique_id"`
	ObjectID            string   `json:"object_id"`
	StateTopic          string   `json:"state_topic"`
	AvailabilityTopic   string   `json:"availability_topic"`
	Device              haDevice `json:"device"`
	UnitOfMeasurement   string   `json:"unit_of_measurement,omitempty"`
	DeviceClass         string   `json:"device_class,omitempty"`
	StateClass          string   `json:"state_class,omitempty"`
	EntityCategory      string   `json:"entity_category,omitempty"`
	Icon                string   `json:"icon,omitempty"`
	PayloadOn           string   `json:"payload_on,omitempty"`
	PayloadOff          string   `json:"payload_off,omitempty"`
	PayloadHome         string   `json:"payload_home,omitempty"`
	PayloadNotHome      string   `json:"payload_not_home,omitempty"`
	SourceType          string   `json:"source_type,omitempty"`
	JSONAttributesTopic string   `json:"json_attributes_topic,omitempty"`
}

func (p *Poller) device() haDevice {
	id := p.wanMAC
	if id == "" {
		id = "unknown"
	}
	model := p.model
	if model == "" {
		model = "Sky Hub"
	}
	return haDevice{
		Identifiers:  []string{"skyhub_" + id},
		Name:         "Sky Hub",
		Manufacturer: "Sagemcom",
		Model:        model,
		SWVersion:    p.firmware,
	}
}

func (p *Poller) entity(component, objectID, name, stateTopic string) haEntity {
	return haEntity{
		Name:              name,
		UniqueID:          "skyhub_" + objectID,
		ObjectID:          "skyhub_" + objectID,
		StateTopic:        stateTopic,
		AvailabilityTopic: p.topic("status"),
		Device:            p.device(),
	}
}

func (p *Poller) discoveryTopic(component, objectID string) string {
	return p.cfg.HAPrefix + "/" + component + "/skyhub/" + objectID + "/config"
}

func (p *Poller) publishEntity(component, objectID string, e haEntity) {
	j, err := json.Marshal(e)
	if err != nil {
		return
	}
	p.publish(p.discoveryTopic(component, objectID), string(j))
}

// publishDiscovery publishes the fixed sensor set.
func (p *Poller) publishDiscovery() {
	type sensor struct {
		id, name, topic, unit, class, stateClass, category, icon string
	}
	sensors := []sensor{
		{"dsl_down_kbps", "DSL downstream rate", p.topic("dsl", "down_kbps"), "kbit/s", "data_rate", "measurement", "", ""},
		{"dsl_up_kbps", "DSL upstream rate", p.topic("dsl", "up_kbps"), "kbit/s", "data_rate", "measurement", "", ""},
		{"noise_margin_down_db", "DSL noise margin down", p.topic("dsl", "noise_margin_down_db"), "dB", "", "measurement", "diagnostic", "mdi:sine-wave"},
		{"noise_margin_up_db", "DSL noise margin up", p.topic("dsl", "noise_margin_up_db"), "dB", "", "measurement", "diagnostic", "mdi:sine-wave"},
		{"attenuation_down_db", "DSL attenuation down", p.topic("dsl", "attenuation_down_db"), "dB", "", "measurement", "diagnostic", "mdi:sine-wave"},
		{"uptime", "Uptime", p.topic("system", "uptime_s"), "s", "duration", "", "diagnostic", ""},
		{"wan_uptime", "WAN uptime", p.topic("wan", "uptime_s"), "s", "duration", "", "diagnostic", ""},
		{"wan_ip", "WAN IPv4", p.topic("wan", "ip"), "", "", "", "diagnostic", "mdi:ip-network"},
		{"firmware", "Firmware", p.topic("system", "firmware"), "", "", "", "diagnostic", "mdi:chip"},
	}
	for _, port := range []struct{ key, name string }{{"wan", "WAN"}, {"lan", "LAN"}, {"wlan24", "WLAN 2.4 GHz"}, {"wlan5", "WLAN 5 GHz"}} {
		sensors = append(sensors,
			sensor{"port_" + port.key + "_tx_bps", port.name + " TX rate", p.topic("port", port.key, "tx_bps"), "bit/s", "data_rate", "measurement", "", ""},
			sensor{"port_" + port.key + "_rx_bps", port.name + " RX rate", p.topic("port", port.key, "rx_bps"), "bit/s", "data_rate", "measurement", "", ""},
		)
	}
	for _, s := range sensors {
		e := p.entity("sensor", s.id, s.name, s.topic)
		e.UnitOfMeasurement = s.unit
		e.DeviceClass = s.class
		e.StateClass = s.stateClass
		e.EntityCategory = s.category
		e.Icon = s.icon
		p.publishEntity("sensor", s.id, e)
	}
	wan := p.entity("binary_sensor", "wan", "WAN connection", p.topic("wan", "status"))
	wan.PayloadOn = "up"
	wan.PayloadOff = "down"
	wan.DeviceClass = "connectivity"
	p.publishEntity("binary_sensor", "wan", wan)
}

// publishTrackerDiscovery publishes a device_tracker entity for one client.
func (p *Poller) publishTrackerDiscovery(key string, st *deviceState) {
	name := st.Hostname
	if name == "" {
		name = st.MAC
	}
	e := p.entity("device_tracker", "tracker_"+key, name, p.topic("device", key, "state"))
	e.PayloadHome = "home"
	e.PayloadNotHome = "not_home"
	e.SourceType = "router"
	e.JSONAttributesTopic = p.topic("device", key, "attributes")
	p.publishEntity("device_tracker", "tracker_"+key, e)
}
