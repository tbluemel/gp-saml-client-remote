gp-saml-client-remote
=====================

Table of Contents
=================

* [Introduction](#introduction)
* [Installation](#installation)
  * [Server](#server)
  * [Client script](#client-script)
* [How to use](#how-to-use)
* [License](#license)

Introduction
============

This project comprises of two parts. A server application, written in go, that is meant to run on a device and/or inside a container that will run the `openconnect` VPN client. The server will basically manage the `openconnect` process, start it and stop it as instructed by the client script.

Additionally, there is a stand-alone shell script `gp-saml-client-remote-client.sh` that is used to talk to the server in order to connect or disconnect the `openconnect` GlobalProtect VPN connection on the remote box. When a SAML login is required, the server will notify the client script, which then uses [gp-saml-gui](https://github.com/dlenski/gp-saml-gui) to pop up the login web page, and the script then sends the results to the server who then continues the login process on the remote machine. While a web browser is still needed on the machine that initiates the connection using the `gp-saml-client-remote-client.sh` script, this enables establishing the VPN connection on a headless system where no web browser or GUI is available.

Installation
============

Server
------

Assuming you have [go installed](https://go.dev/doc/install), simply run `go build .` inside this repository. This generates the `gp-saml-client-remote-server` binary.

The server by default listens on all interfaces on port `8080`. You can configure this by setting the environment variable `GP_SAML_CLIENT_REMOTE_SERVER_LISTEN`, e.g. if you want to limit the server from listening only on localhost you could use something like `export GP_SAML_CLIENT_REMOTE_SERVER_LISTEN=127.0.0.1:8080` before starting the server.

You may set it up to run automatically, e.g. using `systemd`, `supervisor` or other methods.

Client script
-------------

The client script `gp-saml-client-remote-client.sh` can simply be copied to whatever Linux system you'd like to run it on. The script expects a configuration file named `$HOME/.gp-saml-client-remote-client-rc`. Copy the example file [.gp-saml-client-remote-client-rc.sample](.gp-saml-client-remote-client-rc.sample) from this repository into your `$HOME` directory and remove the `.sample` file extension. Then edit the contents as needed.

This script also needs the [gp-saml-gui](https://github.com/dlenski/gp-saml-gui) application. Download it and install it according to the instructions. Be sure you point the variable `GP_SAML_GUI_BIN` in your `$HOME/.gp-saml-client-remote-client-rc` to the location of the `gp-saml-gui` binary.

Assuming the `server` application runs on a remote system, you will need to configure how to talk to it in your `$HOME/.gp-saml-client-remote-client-rc` file. It is recommended to use the encrypted `ssh` protocol. You can do this by uncommenting the example `run_curl_command` function at the bottom of the file and adjusting it to your needs. Note, that you will need to have the `curl` command installed on the system that the server application is running on.

Alternatively, but not recommended, you can point the script directly to the server by setting the `GP_VPN_CONTAINER_SERVER` variable in your `$HOME/.gp-saml-client-remote-client-rc`. You will need to have the `curl` command installed on the system that runs the script. This is not recommended as there is no encryption implemented in the server.

How to use
==========

Assuming you have the server installed on a remote system and the client script installed on your Linux desktop, you first want to check if the client script is able to communicate with the server. You can do this by querying the status like this: `gp-saml-client-remote-client.sh status`. If everything is setup properly and it can successfully communicate with the server, you should see `Status: disconnected`.

Then, to establish a connection, simply ask it to connect: `gp-saml-client-remote-client.sh connect`. It will prompt you for any variables not pre-configured in your `$HOME/.gp-saml-client-remote-client-rc` script, as well as the password for the VPN connection. Once you enter your password, the script sends this information to the server on your remote machine, which then initiates the first step of the login process. Once the server indicates that it needs to display the SAML login page, the client script will use `gp-saml-gui` on your desktop to display this page. The results of the login are then once again sent to the server which then will complete the VPN authentication. Once the VPN connection is established, you should see a `Status: connected` and the script exits. You can query the connection status at any time.

To disconnect the VPN connection, ask it to disconnect: `gp-saml-client-remote-client.sh disconnect`. You should see a `Status: disconnected` response.

License
=======

[MIT License](LICENSE)
