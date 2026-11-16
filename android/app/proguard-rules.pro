# Rules the release build needs beyond what the libraries ship in their own consumer rules.

# kotlinx.serialization: the generated serializers and the companions the runtime looks up by name.
-keepattributes *Annotation*, InnerClasses, Signature, EnclosingMethod
-keep,includedescriptorclasses class com.teanode.**$$serializer { *; }
-keepclassmembers class com.teanode.** { *** Companion; }
-keepclasseswithmembers class com.teanode.** { kotlinx.serialization.KSerializer serializer(...); }

# WebRTC calls: the native library calls back into these classes by name.
-keep class org.webrtc.** { *; }
-dontwarn org.webrtc.**

# The map reads its tile sources and preferences by name.
-keep class org.osmdroid.** { *; }
-dontwarn org.osmdroid.**

# Used only by tools that are not part of the app.
-dontwarn javax.annotation.**
-dontwarn org.slf4j.**
-dontwarn org.conscrypt.**
-dontwarn org.bouncycastle.**
-dontwarn org.openjsse.**
