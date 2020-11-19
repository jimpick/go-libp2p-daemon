install:
	sudo install -C -m 0644 ./p2pd.service /etc/systemd/system/p2pd.service
	sudo systemctl daemon-reload
	@echo
	@echo "p2pd service installed. Don't forget to run 'sudo systemctl start p2pd' to start it and 'sudo systemctl enable p2pd' for it to be enabled on startup."

uninstall:
	-sudo systemctl stop p2pd
	-sudo systemctl disable p2pd
	sudo rm -f /etc/systemd/system/p2pd.service
	sudo systemctl daemon-reload

