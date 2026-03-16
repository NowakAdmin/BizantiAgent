package devices

type ScaleConfig struct {
	Model          string `json:"model,omitempty"`
	Transport      string `json:"transport"`
	TCPHost        string `json:"tcp_host,omitempty"`
	TCPPort        int    `json:"tcp_port,omitempty"`
	BindHost       string `json:"bind_host,omitempty"`
	RXPort         int    `json:"rx_port,omitempty"`
	TXPort         int    `json:"tx_port,omitempty"`
	DibalAddr      byte   `json:"dibal_addr,omitempty"` // Dibal K-series scale address (default 1)
	SerialPort     string `json:"serial_port,omitempty"`
	BaudRate       int    `json:"baud_rate,omitempty"`
	DataBits       int    `json:"data_bits,omitempty"`
	Parity         string `json:"parity,omitempty"`
	StopBits       int    `json:"stop_bits,omitempty"`
	FlowControl    string `json:"flow_control,omitempty"`
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

	// Dibal K-series direct TCP fields (used when transport = "dibal_direct").
	// The Lantronix adapter on the scale connects FROM scale TO PC.
	// PC listens on DibalRXPort; scale sends commands to that port.
	DibalAddr     byte   `json:"dibal_addr,omitempty"`      // Scale address (default 1)
	DibalRXPort   int    `json:"dibal_rx_port,omitempty"`   // Port scale connects to for receiving commands (default 3000)
	DibalBindHost string `json:"dibal_bind_host,omitempty"` // Bind host (default 0.0.0.0)
}

// DibalProgramPayload is the payload for the "program_dibal_plu" command.
// It programs a PLU (product article) record directly into the Dibal scale
// over TCP, without requiring Windows Spooler or any Windows driver.
type DibalProgramPayload struct {
	Scale  ScaleConfig   `json:"scale"`
	PLU    DibalPLU      `json:"plu"`
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
