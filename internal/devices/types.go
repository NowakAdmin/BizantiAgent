package devices

type ScaleConfig struct {
	Transport      string `json:"transport"`
	TCPHost        string `json:"tcp_host,omitempty"`
	TCPPort        int    `json:"tcp_port,omitempty"`
	SerialPort     string `json:"serial_port,omitempty"`
	BaudRate       int    `json:"baud_rate,omitempty"`
	RequestCommand string `json:"request_command,omitempty"`
	ReadTimeoutMs  int    `json:"read_timeout_ms,omitempty"`
}

type PrinterConfig struct {
	Model         string `json:"model"`
	Transport     string `json:"transport,omitempty"`
	PrinterName   string `json:"printer_name,omitempty"`
	Host          string `json:"host"`
	Port          int    `json:"port,omitempty"`
	WriteTimeoutS int    `json:"write_timeout_s,omitempty"`
}

type WeighAndPrintPayload struct {
	Scale    ScaleConfig        `json:"scale"`
	Printer  PrinterConfig      `json:"printer"`
	Template string             `json:"template"`
	Context  map[string]string  `json:"context,omitempty"`
	WeightKg *float64           `json:"weight_kg,omitempty"`
	Meta     map[string]any     `json:"meta,omitempty"`
	Tags     map[string]string  `json:"tags,omitempty"`
	RawData  map[string]string  `json:"raw_data,omitempty"`
	Options  map[string]float64 `json:"options,omitempty"`
}
