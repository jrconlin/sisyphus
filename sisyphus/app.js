var endpoint;

function Log(s, cls) {
  console.log(s)
  var txt = document.createTextNode(s);
  var p = document.createElement("p");
  p.className=cls
  p.appendChild(txt);
  document.getElementById("status").appendChild(p);
}

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

function doRegister() {
  Log("Registering...")
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

