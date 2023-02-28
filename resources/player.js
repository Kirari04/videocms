function getM3u8String(file, width, height) {
    return `\n#EXT-X-STREAM-INF:BANDWIDTH=${parseInt(width) * parseInt(height) * 20
        },RESOLUTION=${width}x${height},CODECS="avc1.640015,mp4a.40.2"\n${window.location.origin
        }${file}\n`;
}
function getM3u8Stream(qualitys) {
    let m3u8 = `#EXTM3U\n#EXT-X-VERSION:6`;
    qualitys.forEach((quality) => {
        m3u8 += getM3u8String(quality.file, quality.width, quality.height);
    });

    const blob = new Blob([m3u8], {
        type: "text/plain",
    });
    let url = URL.createObjectURL(blob);
    return url;
}



let dpQualitys = Qualitys.map((Quality) => {
    return {
        name: Quality.label,
        url: Quality.file,
        type: "hls",
    };
});
dpQualitys.push({
    name: "Auto",
    url: getM3u8Stream(Qualitys),
    type: "hls",
});
let defaultQuality = dpQualitys.length -1;


const dp = new DPlayer({
    container: document.getElementById("dplayer"),
    screenshot: false,
    video: {
        defaultQuality: defaultQuality,
        quality: dpQualitys,
    },
    subtitle: {
        url: (!Subtitles) ? [] : Subtitles,
        defaultSubtitle: 0,
        type: 'webvtt',
        fontSize: '4vh',
        bottom: '7%',
        color: '#b7daff'
    },

});

// custom buttons
function addRightButton(className, label, img, callback) {
    const rightControls = document.querySelector(
        ".dplayer-icons.dplayer-icons-right"
    );
    const div = document.createElement("div");
    div.addEventListener("click", callback);
    div.classList.add("dplayer-icon");
    div.classList.add(className);
    div.style.position = "relative";
    div.style.height = "40px";
    div.style.padding = "0px";
    const button = document.createElement("button");
    button.classList.add("dplayer-icon");
    button.classList.add("dplayer-setting-icon");
    button.setAttribute("data-balloon", label);
    button.style.position = "relative";
    button.style.height = "100%";
    const span = document.createElement("span");
    span.style.position = "relative";
    span.style.height = "100%";
    span.style.color = "#fff";
    span.style.display = "inline-block";
    span.classList.add("dplayer-icon-content");
    span.innerHTML = `<img src="${img}" style="height: 100%;">`;

    button.appendChild(span);
    div.appendChild(button);
    rightControls.prepend(div);
}

addRightButton("dplayer-cool", "Forward 10 sec", "/plus.svg", () =>
    dp.seek(dp.video.currentTime + 10)
);
addRightButton("dplayer-cool", "Rewind 10 sec", "/minus.svg", () =>
    dp.seek(dp.video.currentTime - 10)
);

// save position
setInterval(() => {
    if (!dp.video.paused) {
        localStorage.setItem(`POSITION-${UUID}`, dp.video.currentTime);
    }
}, 1000);

const oldPosition = parseInt(localStorage.getItem(`POSITION-${UUID}`));
if (oldPosition > 0) {
    Swal.fire({
        title: "Do you want to start where you left last time?",
        showDenyButton: true,
        confirmButtonText: "Yes",
        denyButtonText: "No",
    }).then((result) => {
        if (result.isConfirmed) {
            dp.seek(oldPosition);
        }
    });
}
if (EncQualitys) {
    EncQualitys.forEach(EncQuality => {
        dp.notice(`The server is still encoding the quality ${EncQuality.label} (${Math.round(parseFloat(EncQuality.progress) * 100)}%)`, 10000, 0.8);
    })
}