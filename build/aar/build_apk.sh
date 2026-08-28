#!/bin/bash
set -e
cd "$(dirname "$0")/../.."

ANDROID_HOME="${ANDROID_HOME:-$HOME/android-sdk}"
export ANDROID_HOME
export ANDROID_SDK_ROOT="$ANDROID_HOME"

EBITENMOBILE="${EBITENMOBILE:-$(command -v ebitenmobile || echo "$(go env GOPATH)/bin/ebitenmobile")}"

ANDROID_JAR=$ANDROID_HOME/platforms/android-34/android.jar
BUILD_TOOLS=$ANDROID_HOME/build-tools/34.0.0
AAPT=$BUILD_TOOLS/aapt
D8=$BUILD_TOOLS/d8
APKSIGNER=$BUILD_TOOLS/apksigner
KEYSTORE=debug.keystore
APK_NAME=godwarf.apk
AAR_DIR="$(pwd)/build/aar"

mkdir -p "$AAR_DIR"

echo "=== Compile Go ==="
"$EBITENMOBILE" bind -target android -javapkg xyz.m45sci.godwarf -o "$AAR_DIR/godwarf.aar" ./mobile

echo "=== Extract AAR ==="
cd "$AAR_DIR"
rm -rf jni classes.jar R.txt proguard.txt AndroidManifest.xml
unzip -o godwarf.aar
cd ../..

echo "=== Copy app icons into AAR ==="
mkdir -p "$AAR_DIR/res"
cp -r android/res/* "$AAR_DIR/res/"

echo "=== Patch MainActivity.java ==="
MAIN_JAVA="$AAR_DIR/src/xyz/m45sci/godwarf/MainActivity.java"
mkdir -p "$(dirname "$MAIN_JAVA")"

cat > "$MAIN_JAVA" << 'JAVAEOF'
package xyz.m45sci.godwarf;

import android.app.Activity;
import android.content.Intent;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.Environment;
import android.provider.Settings;
import android.view.View;
import android.view.Window;
import android.view.WindowManager;
import android.util.Log;
import java.io.FileWriter;
import java.io.IOException;
import java.io.File;

import go.Seq;
import xyz.m45sci.godwarf.mobile.EbitenView;

public class MainActivity extends Activity {
    private static final String TAG = "goDwarf";
    private static final String LOG_PATH = "/storage/emulated/0/Documents/goDwarf/debuglogs/godwarf_java.log";

    private void log(String msg) {
        Log.d(TAG, msg);
        try {
            File f = new File(LOG_PATH);
            f.getParentFile().mkdirs();
            FileWriter fw = new FileWriter(f, true);
            fw.write(android.os.Process.myPid() + " " + System.currentTimeMillis() + " " + msg + "\n");
            fw.close();
        } catch (IOException e) {
        }
    }

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        log("onCreate");

        Seq.setContext(getApplicationContext());
        log("Seq.setContext done");

        requestWindowFeature(Window.FEATURE_NO_TITLE);
        getWindow().setFlags(
            WindowManager.LayoutParams.FLAG_FULLSCREEN,
            WindowManager.LayoutParams.FLAG_FULLSCREEN
        );

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.KITKAT) {
            getWindow().getDecorView().setSystemUiVisibility(
                View.SYSTEM_UI_FLAG_LAYOUT_STABLE
                | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION
                | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION
                | View.SYSTEM_UI_FLAG_FULLSCREEN
                | View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
            );
        }

        EbitenView view = new EbitenView(this);
        setContentView(view);
        log("EbitenView set as content view");

        requestStoragePermission();
    }

    @Override
    protected void onResume() {
        super.onResume();
        hideSystemUI();
    }

    @Override
    public void onWindowFocusChanged(boolean hasFocus) {
        super.onWindowFocusChanged(hasFocus);
        if (hasFocus) {
            hideSystemUI();
        }
    }

    private void hideSystemUI() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.KITKAT) {
            getWindow().getDecorView().setSystemUiVisibility(
                View.SYSTEM_UI_FLAG_LAYOUT_STABLE
                | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION
                | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION
                | View.SYSTEM_UI_FLAG_FULLSCREEN
                | View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
            );
        }
    }

    private void requestStoragePermission() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            if (!Environment.isExternalStorageManager()) {
                log("requestStoragePermission: launching settings");
                try {
                    Intent intent = new Intent(Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION);
                    intent.setData(Uri.parse("package:" + getPackageName()));
                    startActivity(intent);
                } catch (Exception e) {
                    log("requestStoragePermission: specific intent failed: " + e);
                    try {
                        Intent intent = new Intent(Settings.ACTION_MANAGE_ALL_FILES_ACCESS_PERMISSION);
                        startActivity(intent);
                    } catch (Exception e2) {
                        log("requestStoragePermission: generic intent failed: " + e2);
                    }
                }
            } else {
                log("requestStoragePermission: already granted");
            }
        }
    }
}
JAVAEOF

WORK=/tmp/godwarf-apk-build
rm -rf "$WORK"
mkdir -p "$WORK/res" "$WORK/obj" "$WORK/dex"

cat > "$WORK/AndroidManifest.xml" << 'MANIFEST'
<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android"
    package="xyz.m45sci.godwarf"
    android:versionCode="1"
    android:versionName="1.0">
    <uses-sdk android:minSdkVersion="21" android:targetSdkVersion="33"/>
    <uses-permission android:name="android.permission.INTERNET"/>
    <uses-permission android:name="android.permission.ACCESS_NETWORK_STATE"/>
    <uses-permission android:name="android.permission.MANAGE_EXTERNAL_STORAGE"/>
    <uses-feature android:glEsVersion="0x00020000" android:required="true"/>
    <application
        android:label="goDwarf"
        android:icon="@mipmap/ic_launcher"
        android:hasCode="true"
        android:requestLegacyExternalStorage="true"
        android:theme="@android:style/Theme.NoTitleBar.Fullscreen">
        <activity
            android:name="xyz.m45sci.godwarf.MainActivity"
            android:configChanges="orientation|keyboardHidden|screenSize"
            android:screenOrientation="landscape"
            android:windowSoftInputMode="adjustNothing"
            android:exported="true">
            <intent-filter>
                <action android:name="android.intent.action.MAIN"/>
                <category android:name="android.intent.category.LAUNCHER"/>
            </intent-filter>
        </activity>
    </application>
</manifest>
MANIFEST

cp -r "$AAR_DIR/jni" "$WORK/"
cp "$AAR_DIR/classes.jar" "$WORK/"
cp -r "$AAR_DIR/res" "$WORK/"

echo "=== Compile Java ==="
javac -source 1.8 -target 1.8 \
  -classpath "$ANDROID_JAR:$AAR_DIR/src:$WORK/classes.jar" \
  -d "$WORK/obj" \
  "$AAR_DIR/src/xyz/m45sci/godwarf/MainActivity.java"

echo "=== D8 dex ==="
"$D8" --output "$WORK/dex" \
  --lib "$ANDROID_JAR" \
  --min-api 21 \
  "$WORK/obj/xyz/m45sci/godwarf/MainActivity.class" \
  "$WORK/classes.jar"

echo "=== Package APK ==="
"$AAPT" package -f \
  -M "$WORK/AndroidManifest.xml" \
  -S "$WORK/res" \
  -I "$ANDROID_JAR" \
  -F "$WORK/godwarf-unsigned.apk"

echo "=== Add native libs and dex ==="
(
  cd "$WORK"
  mkdir -p lib/armeabi-v7a lib/arm64-v8a lib/x86 lib/x86_64
  cp jni/armeabi-v7a/libgojni.so lib/armeabi-v7a/
  cp jni/arm64-v8a/libgojni.so lib/arm64-v8a/
  cp jni/x86/libgojni.so lib/x86/
  cp jni/x86_64/libgojni.so lib/x86_64/
  zip -r -0 godwarf-unsigned.apk lib/
  cp dex/classes.dex .
  zip -r -0 godwarf-unsigned.apk classes.dex
)

echo "=== Zipalign ==="
"$BUILD_TOOLS/zipalign" -f 4 "$WORK/godwarf-unsigned.apk" "$WORK/godwarf-aligned.apk"

echo "=== Generate debug keystore ==="
if [ ! -f "$AAR_DIR/$KEYSTORE" ]; then
  keytool -genkeypair -v -keystore "$AAR_DIR/$KEYSTORE" \
    -alias godwarf -keyalg RSA -keysize 2048 -validity 10000 \
    -storepass android -keypass android \
    -dname "CN=goDwarf,O=goDwarf,C=US"
fi

echo "=== Sign APK ==="
"$APKSIGNER" sign --ks "$AAR_DIR/$KEYSTORE" \
  --ks-key-alias godwarf \
  --ks-pass pass:android \
  --key-pass pass:android \
  --out "$AAR_DIR/$APK_NAME" \
  "$WORK/godwarf-aligned.apk"

echo "=== Done ==="
ls -la "$AAR_DIR/$APK_NAME"