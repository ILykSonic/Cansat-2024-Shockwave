package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"github.com/gorilla/websocket" // WebSocket package for managing connections
	"github.com/tarm/serial"       // Serial package for handling serial port communications
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var conn *websocket.Conn //WebSocket connection
var mutex sync.Mutex
var port *serial.Port     //serial port
var pressure_flag = false // flag to control periodic sending of data
var isEnabled = false     // flag to check if the system is enabled to send data

func wsHandler(w http.ResponseWriter, r *http.Request) {
	upgraderConn, err := upgrader.Upgrade(w, r, nil) // Upgrade the HTTP server connection to a WebSocket connection
	if err != nil {
		log.Printf("Error during connection upgrade: %v", err)
		return
	}

	mutex.Lock()
	conn = upgraderConn
	mutex.Unlock()

	go func(c *websocket.Conn) {
		defer func() {
			mutex.Lock()
			c.Close()
			if conn == c {
				conn = nil
			}
			mutex.Unlock()
		}()

		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Printf("Error reading from WebSocket: %v", err)
				break
			}

			log.Printf("Command Recieved: %s", string(message))

			messageStr := string(message) //The command I'm sending
			switch messageStr {
			case "CMD,2078,SIM,EN,GOAT":
				isEnabled = true
			case "CMD,2078,SIM,AC,GOAT":
				if isEnabled {
					log.Println("Received start simulation command; delaying start...")
					pressure_flag = true
					go sim_serial()
				}
			case "CMD,2078,SIM,DS,GOAT":
				pressure_flag = false
				log.Println("Simulation mode stopped by 'stop_sim_mode' command.")
			}

			mutex.Lock()
			if port != nil {
				_, err = port.Write(message)
				if err != nil {
					log.Printf("Error sending to serial port: %v", err)
				}
			} else {
				log.Printf("Serial port not initialized or closed.\n")
			}
			mutex.Unlock()
		}
	}(conn)
}

// main sets up the HTTP server and initializes the WebSocket endpoint and serial port.
func main() {
	http.HandleFunc("/ws", wsHandler)

	c := &serial.Config{Name: "COM5", Baud: 9600} //change comport when needed
	var err error
	port, err = serial.OpenPort(c)
	if err != nil {
		log.Fatalf("Error opening serial port: %v", err)
	}
	defer port.Close()

	go func() {
		log.Fatal(http.ListenAndServe("localhost:2078", nil)) // Start the HTTP server
	}()

	csv_write() // Start reading from the serial port and writing to CSV
}

// sim_serial sends data from a CSV file through the serial port every second.
func sim_serial() {
	file, err := os.Open("cansat_2024_simp.csv")
	if err != nil {
		log.Fatalf("Error opening CSV file: %v", err)
	}
	defer file.Close()

	csvReader := csv.NewReader(file)
	records, err := csvReader.ReadAll()
	if err != nil {
		log.Fatalf("Error reading CSV records: %v", err)
	}

	ticker := time.NewTicker(1 * time.Second)

	//time.Sleep(5 * time.Second) // Corrected from `timeSecond` to `time.Second`
	defer ticker.Stop()

	sim_row := 1 // Start from the second row, assuming the first row contains headers

	for range ticker.C {
		if !pressure_flag {
			break
		}

		if sim_row >= len(records) {
			sim_row = 1 // Loop back to the first row if the end of the records is reached
		}

		sim_row_data := records[sim_row]

		// Create a single string from all columns in the row
		full_sim_data := strings.Join(sim_row_data, ",") + ",GOAT" // Join all columns into a single string

		mutex.Lock()
		if port != nil && full_sim_data != "" {
			_, err := port.Write([]byte(full_sim_data))
			if err != nil {
				log.Printf("Error sending full row data to serial port: %v", err)
			}
		}
		mutex.Unlock()

		sim_row++
	}
}

// csv_write reads data from the serial port and writes it to a CSV file.
func csv_write() {
	reader := bufio.NewReader(port)
	var buffer strings.Builder
	file, err := os.OpenFile("Flight_2078.csv", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("Error opening or creating CSV file: %v", err)
	}
	defer file.Close()

	csvWriter := csv.NewWriter(file)
	defer csvWriter.Flush()

	fileInfo, err := file.Stat()
	if err != nil {
		log.Fatalf("Error getting file info: %v", err)
	}
	if fileInfo.Size() == 0 {
		headers := []string{"<TEAM ID>", "<MISSION_TIME>", "<PACKET_COUNT>", "<MODE>", "<STATE>", "<ALTITUDE>",
			"<AIR SPEED>", "<HS_DEPLOYED>", "<PC_DEPLOYED>", "<TEMPERATURE>", "<VOLTAGE>", "<PRESSURE>", "<GPS_TIME>",
			"<GPS_ALTITUDE>", "<GPS_LATITUDE>", "<GPS_LONGITUDE>", "<GPS_SATS>", "<TILT_X>", "<TILT_Y>",
			"<ROT_Z>", "<CMD_ECHO>", "<MT>", "<PID_STATE>", "<GOAT>"}
		if err := csvWriter.Write(headers); err != nil { // Write headers to the new CSV file
			log.Fatalf("Error writing headers to CSV: %v", err)
		}
		csvWriter.Flush()
	}

	for {
		data, err := reader.ReadByte()
		if err != nil {
			log.Printf("Error reading from serial port: %v", err)
			continue
		}

		buffer.WriteByte(data)
		bufferedData := buffer.String()

		if strings.Contains(bufferedData, "GOAT") {
			eop_index := strings.Index(bufferedData, "GOAT") + len("GOAT")
			complete_packet := bufferedData[:eop_index]

			cleanData := start_with_2078(complete_packet)
			if cleanData != "" {
				record := strings.Split(cleanData, ",")
				if err := csvWriter.Write(record); err != nil { // Write the cleaned data to the CSV file
					log.Printf("Error writing record to CSV: %v", err)
				}

				csvWriter.Flush()
				if err := csvWriter.Error(); err != nil {
					log.Printf("Error flushing writer after write: %v", err)
				}

				fmt.Printf("%s\n", cleanData)

				// Send the clean data over WebSocket if a connection exists
				mutex.Lock()
				if conn != nil {
					if err := conn.WriteMessage(websocket.TextMessage, []byte(cleanData)); err != nil { // Send the clean data over WebSocket
						log.Printf("Error sending data over WebSocket: %v", err)
					}
				}
				mutex.Unlock()
			}

			if eop_index < len(bufferedData) {
				buffer.Reset()
				buffer.WriteString(bufferedData[eop_index:])
			} else {
				buffer.Reset()
			}
		}
	}
}

// start_with_2078 removes unwanted characters from a packet of data starting with the marker "2078".
func start_with_2078(str string) string {
	startIndex := strings.Index(str, "2078")
	if startIndex == -1 {
		return ""
	}

	valid_packet_data := str[startIndex:]
	return strings.Replace(valid_packet_data, "~}3AA", "", -1)
}
