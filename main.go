package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("\n=== Cliente de Juegos P2P ===")
	
	client := NewGameClient()
	if err := client.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
} 