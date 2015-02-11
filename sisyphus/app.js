/* This Source Code Form is subject to the terms of the Mozilla Public
 *  * License, v. 2.0. If a copy of the MPL was not distributed with this
 *   * file, You can obtain one at http://mozilla.org/MPL/2.0/. */

var endpoint;

/** Stupid logging function
 */
function Log(s, cls) {
  console.log(s)
  var txt = document.createTextNode(s);
  var p = document.createElement("p");
  p.className=cls
  p.appendChild(txt);
  document.getElementById("status").appendChild(p);
}

/** Send info to the push server
 */
function send(s, path='/reg', method='POST') {
  var host = document.getElementById("server").value;
  if (!host.startsWith('http')) {
      host = 'http://'+ host;
  }
  var name = document.getElementById("name").value;
  if (host == "") {
    Log ("Host not specified","error")
    return false
  }
  var post = new XMLHttpRequest();
  post.open(method, host + path);
  post.setRequestHeader("Content-Type", "application/x-www-form-urlencoded");
  post.onload=function(e) {
    if (this.status != 200) {
      Log("Error !200: " + JSON.stringify(e) + JSON.stringify(this))
    }
    Log("Registered.")
  }
  post.onerror=function(e) {
    Log("Error. Check server log.", "error")
  }
  post.send("sp="+encodeURIComponent(s)+"&name="+encodeURIComponent(name));
  return true
}

/** Set the message handler for the push events
 */
function setMessageHandler() {
  navigator.mozSetMessageHandler('push', function(e) {
    Log("Recv'd push #"+e.version+"!")
    send(e.pushEndpoint, '/ack')
  })
  navigator.mozSetMessageHandler("push-register", function(e){
    Log("Recv'd re-registration "+JSON.stringify(e))
    send(endpoint, '/ack', 'DELETE')
    doRegister()
  })
  Log("Message Handler Set.")
}

/** Register the endpoint with the server
 */
function doRegister() {
  Log("Registering...");
  var srv = document.getElementById("server");
  if (srv.value.length == 0) {
    alert("Server address missing.");
    if (!srv.classList.contains("error")) {
        srv.classList.add("error");
    }
    return;
  }
  srv.classList.remove("error");
  var req = navigator.push.register();
  req.onsuccess = function(e) {
    endpoint = req.result;
    Log("Endpoint:" + endpoint)
    send(endpoint, '/reg')
  }
  req.onerror = function(e) {
      Log("Registration error: " + JSON.stringify(e), "error");
      return;
  }
}

// main
if (!navigator.push && !navigator.mozSetMessageHandler) {
  document.getElementById("config").style.display="none";
  Log("No push service.")
} else {
    setMessageHandler();
    document.getElementById("go").addEventListener("click", doRegister, true)
}

