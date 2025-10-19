package main

func main() {
	new_config()

	logger := new_logger()
	logger.Debug().Msg("Debug logging enabled")
}
