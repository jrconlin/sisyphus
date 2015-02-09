var endpoint;

function Log(s) {
  console.log(s)
  txt = document.createTextNode(s);
  document.getElementById("log").appendChild(txt);
}

function send(s, path='/reg', method='POST') {
  var host = document.getElementById("server").value;
  var name = document.getElementById("name").value;
  if host == "" {
    Log ("Host not specified")
    return false
  }
  var post = new XMLHttpRequest();
  post.open(method, host + path);
  post.onload=function(e) {
    if (this.status != 200) {
      Log("Error: " + JSON.stringify(e))
    }
  }
  post.onerror=function(e) {
    Log("Error: " + e)
  }
  post.send("sp="+encodeURIComponent(host)+"&name="+encodeURIComponent(name));
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
}

function doRegister() {
  var req = navigator.push.register();
  req.onsuccess = function(e) {
    endpoint = req.result;
    Log("Endpoint:" + endpoint)
    send(endpoint, '/reg')
  }
}

// main
if (!navigator.push  || !navigator.mozSetMessageHandler) {
  alert("Sorry, push is not available");
  document.getElementsByTagName("body").innerHTML="<h1>Sorry, push is not available.</h1>";
  return;
}

setMessageHandler();
document.getElementById("go").addEventListener("click", doRegister, true)


