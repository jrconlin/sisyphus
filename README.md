#Project Sisyphus

Ok, *this* project Sisyphus is about creating a tester app that
monitors SimplePush connections to devices.

There's an outstanding bug with FirefoxOS devices where the TCP
connection fails after some time. This results in the device no longer
getting Push notifications.

This service forever pushes and screams when it can no longer.

So, yeah, the name seemed fairly apt.

## The bits

There are two components to Sisyphus. The first is the server/UI
(located in ./server).

### The Server

This uses golang and sqlite3 and builds a server that will run on
http://localhost:8080. (sqlite3 is just used as a cheap database. Feel
free to fork and use your data store of choice.)

```test_server -h``` for a list of commands.

Once the server is running, open a browser window to the correct host
and port (e.g. ```http://localhost:8080```).

The []Play next alert message will have the page play a loud sound
when one or more clients is determined to no longer be connected.

### The Client

The client code is located in the (./sisyphus) directory. Probably the
easiest way to run it is to:
* Launch the WebIDE in Firefox
* [Open App v]
* Open Packaged App...
* (Navigate to the sisyphus/sisyphus directory)
* [Open Directory]

At that point, you should see the app project in the IDE. To run it:

* [Select Runtime v]
* Firefox 2.0 (or later. You can download using the *Install
  Simulators* menu option)
* Once the simulator is running, you can click the "Play" triangle.

In the emulator, you'll need to set the Host to your server's URL. You
may also provide a friendlier name for your client, and click [Go!]

And thus, magic ensues.

You can also load the Sisyphus app onto a device using the WebIDE and
launch there.

