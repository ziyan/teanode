// teanode-faces finds the faces in one picture and prints a vector for each,
// so that the server can tell which pictures show the same person.
//
// The picture arrives on standard input as a binary PPM (P6), not as a JPEG or
// PNG: reading raw pixels needs only OpenCV's core, imgproc, dnn and objdetect
// libraries, where its image decoders would drag in about 170 MB more. The
// server decodes the picture itself and writes it out this way.
//
// Faces are found by YuNet and described by SFace, both from the OpenCV model
// zoo. Two vectors of one person have a cosine similarity above about 0.36.

#include <opencv2/core.hpp>
#include <opencv2/imgproc.hpp>
#include <opencv2/objdetect/face.hpp>

#include <cctype>
#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <iomanip>
#include <iostream>
#include <sstream>
#include <string>

namespace {

const char* const kDefaultDetectorPath = "/usr/share/teanode/faces/face_detection_yunet_2023mar.onnx";
const char* const kDefaultRecognizerPath = "/usr/share/teanode/faces/face_recognition_sface_2021dec.onnx";

// A 40 megapixel picture is 120 MB of pixels, and the server never sends more
// than a large preview; the limit is here so that a mistake cannot take the
// machine's memory.
const int64_t kMaximumPixels = 40LL * 1000 * 1000;

// What the detector keeps: faces it is at least this sure of, with overlapping
// boxes merged when they overlap by more than the second number.
const float kScoreThreshold = 0.8f;
const float kNonMaximumSuppressionThreshold = 0.3f;
const int kTopCount = 5000;

// Failure is a message on standard error and exit status 1, which the server
// shows to whoever asks why a picture has no faces.
[[noreturn]] void Fail(const std::string& message) {
    std::cerr << "teanode-faces: " << message << std::endl;
    std::exit(1);
}

// Reads one whitespace-delimited word of a PPM header, skipping comments (a #
// to the end of the line). It consumes the one whitespace character after the
// word, which for the last number of the header is the single one before the
// pixels.
std::string ReadHeaderWord(std::istream& input) {
    std::string word;
    int next;
    while ((next = input.get()) != EOF) {
        if (next == '#') {
            while ((next = input.get()) != EOF && next != '\n') {
            }
            continue;
        }
        if (std::isspace(next)) {
            if (word.empty()) {
                continue;
            }
            return word;
        }
        word.push_back(static_cast<char>(next));
        if (word.size() > 32) {
            Fail("the PPM header is not valid");
        }
    }
    if (word.empty()) {
        Fail("the PPM header is cut short");
    }
    return word;
}

int64_t ParseHeaderNumber(const std::string& word) {
    if (word.empty() || word.size() > 10) {
        Fail("the PPM header holds a number that is not valid");
    }
    int64_t value = 0;
    for (char digit : word) {
        if (digit < '0' || digit > '9') {
            Fail("the PPM header holds a number that is not valid");
        }
        value = value * 10 + (digit - '0');
    }
    return value;
}

// Reads a binary PPM with maximum value 255 into an OpenCV image in BGR order,
// which is the order OpenCV's models expect.
cv::Mat ReadPicture(std::istream& input) {
    if (ReadHeaderWord(input) != "P6") {
        Fail("the picture is not a binary PPM (P6)");
    }
    const int64_t width = ParseHeaderNumber(ReadHeaderWord(input));
    const int64_t height = ParseHeaderNumber(ReadHeaderWord(input));
    const int64_t maximum = ParseHeaderNumber(ReadHeaderWord(input));
    if (width < 1 || height < 1) {
        Fail("the picture has no pixels");
    }
    if (maximum != 255) {
        Fail("the PPM maximum value must be 255");
    }
    if (width > kMaximumPixels || height > kMaximumPixels || width * height > kMaximumPixels) {
        Fail("the picture is larger than 40 megapixels");
    }

    cv::Mat rgb(static_cast<int>(height), static_cast<int>(width), CV_8UC3);
    const std::streamsize expectedBytes = static_cast<std::streamsize>(width * height * 3);
    input.read(reinterpret_cast<char*>(rgb.data), expectedBytes);
    if (input.gcount() != expectedBytes) {
        Fail("the picture ends before all of its pixels");
    }
    cv::Mat bgr;
    cv::cvtColor(rgb, bgr, cv::COLOR_RGB2BGR);
    return bgr;
}

// JSON has no NaN or infinity; a detector does not produce them, but a
// number that would break the output is printed as 0 rather than trusted.
void PrintNumber(std::ostream& output, double value) {
    output << (std::isfinite(value) ? value : 0.0);
}

}  // namespace

int main(int argc, char** argv) {
    std::string detectorPath = kDefaultDetectorPath;
    std::string recognizerPath = kDefaultRecognizerPath;
    for (int index = 1; index < argc; index++) {
        const std::string argument = argv[index];
        if ((argument == "--detector" || argument == "--recognizer") && index + 1 < argc) {
            (argument == "--detector" ? detectorPath : recognizerPath) = argv[++index];
        } else {
            std::cerr << "usage: teanode-faces [--detector <path>] [--recognizer <path>] < picture.ppm" << std::endl;
            return argument == "--help" ? 0 : 1;
        }
    }

    try {
        const cv::Mat image = ReadPicture(std::cin);

        cv::Ptr<cv::FaceDetectorYN> detector =
            cv::FaceDetectorYN::create(detectorPath, "", image.size(), kScoreThreshold, kNonMaximumSuppressionThreshold, kTopCount);
        cv::Ptr<cv::FaceRecognizerSF> recognizer = cv::FaceRecognizerSF::create(recognizerPath, "");

        // One row per face: x, y, width, height, five landmarks (x, y each),
        // and the score last.
        cv::Mat faces;
        detector->detect(image, faces);

        // The shortest form that reads back as the same number: 9 significant
        // digits for a float.
        std::ostringstream output;
        output << std::setprecision(9);
        output << "{\"widthPixels\":" << image.cols << ",\"heightPixels\":" << image.rows << ",\"faces\":[";
        for (int row = 0; row < faces.rows; row++) {
            // Aligning turns the face upright and to a fixed size by its
            // landmarks, which is what the recognizer was trained on.
            cv::Mat aligned;
            recognizer->alignCrop(image, faces.row(row), aligned);
            cv::Mat feature;
            recognizer->feature(aligned, feature);
            feature = feature.clone();
            // Unit length, so that a dot product is a cosine similarity.
            cv::normalize(feature, feature);

            if (row > 0) {
                output << ",";
            }
            output << "{\"boxLeft\":";
            PrintNumber(output, faces.at<float>(row, 0));
            output << ",\"boxTop\":";
            PrintNumber(output, faces.at<float>(row, 1));
            output << ",\"boxWidth\":";
            PrintNumber(output, faces.at<float>(row, 2));
            output << ",\"boxHeight\":";
            PrintNumber(output, faces.at<float>(row, 3));
            output << ",\"detectionScore\":";
            PrintNumber(output, faces.at<float>(row, 14));
            output << ",\"faceVector\":[";
            for (int column = 0; column < feature.cols; column++) {
                if (column > 0) {
                    output << ",";
                }
                PrintNumber(output, feature.at<float>(0, column));
            }
            output << "]}";
        }
        output << "]}" << std::endl;
        std::cout << output.str();
    } catch (const cv::Exception& error) {
        Fail(std::string("OpenCV failed: ") + error.what());
    } catch (const std::exception& error) {
        Fail(error.what());
    }
    return 0;
}
