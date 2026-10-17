# teanode-faces

A small C++ program that finds the faces in one picture and prints a vector
for each, so that the server can tell which photos show the same person. The
server runs it as a child process (`internal/faces`) and stays free of C code.

## What it does

It reads a binary PPM (`P6`, maximum value 255, `#` comment lines allowed in
the header) on standard input and prints one line of JSON on standard output:

    {"widthPixels":640,"heightPixels":480,"faces":[
      {"boxLeft":212.5,"boxTop":96.1,"boxWidth":120.4,"boxHeight":150.2,
       "detectionScore":0.93,"faceVector":[ ...128 numbers... ]}]}

Boxes are in pixels from the top left corner; the Go side turns them into
fractions of the picture. A face vector has length 1, so the dot product of
two is their cosine similarity. SFace's threshold for "the same person" is
about 0.363.

It reads PPM instead of an encoded picture on purpose: OpenCV's image decoders
(`imgcodecs`) pull in GDAL, HDF5 and more, about 170 MB of libraries, where
`core`, `imgproc`, `dnn` and `objdetect` are 42 MB. The server decodes the
picture itself and writes it out as a PPM (`picture.EncodePPM`).

A picture of more than 40 megapixels, a header that is not a PPM and a body
that is shorter than the header says are refused with a message on standard
error and exit status 1.

Options: `--detector <path>` and `--recognizer <path>`, which default to the
two files below in `/usr/share/teanode/faces/`.

## Building

On Debian trixie (OpenCV 4.10):

    apt-get install g++ libopencv-objdetect-dev libopencv-dnn-dev \
        libopencv-imgproc-dev libopencv-core-dev
    g++ -O2 -std=c++17 teanode-faces.cpp -o teanode-faces -I/usr/include/opencv4 \
        -lopencv_objdetect -lopencv_dnn -lopencv_imgproc -lopencv_core

`deploy/Dockerfile` builds it in its `faces-reader` stage and copies only the
program, the libraries it links and the models into the runtime image, not the
compiler or the development packages.

## Models

Both come from the [OpenCV model zoo](https://github.com/opencv/opencv_zoo).
The Dockerfile downloads them and refuses a file whose SHA-256 differs.

| Model | File | License | SHA-256 |
| --- | --- | --- | --- |
| YuNet, finds the faces | `face_detection_yunet_2023mar.onnx` | MIT | `8f2383e4dd3cfbb4553ea8718107fc0423210dc964f9f4280604804ed2552fa4` |
| SFace, describes them | `face_recognition_sface_2021dec.onnx` | Apache 2.0 | `0ba9fbfa01b5270c96627c4ef784da859931e02f04419c829e83484087c34e79` |

The detector keeps faces it scores at 0.8 or more, merges boxes that overlap
by more than 0.3, and keeps at most 5000.
