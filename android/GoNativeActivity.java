package org.golang.app;

import android.app.NativeActivity;
import android.content.Intent;
import android.content.pm.ActivityInfo;
import android.content.pm.PackageManager;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.Environment;
import android.os.Handler;
import android.os.Looper;
import android.provider.Settings;
import android.view.KeyCharacterMap;

public class GoNativeActivity extends NativeActivity {

	String getTmpdir() {
		return getCacheDir().getAbsolutePath();
	}

	static int getRune(int deviceId, int keyCode, int metaState) {
		try {
			int rune = KeyCharacterMap.load(deviceId).get(keyCode, metaState);
			if (rune == 0) return -1;
			return rune;
		} catch (KeyCharacterMap.UnavailableException e) {
			return -1;
		} catch (Exception e) {
			return -1;
		}
	}

	private void load() {
		try {
			ActivityInfo ai = getPackageManager().getActivityInfo(
					getIntent().getComponent(), PackageManager.GET_META_DATA);
			if (ai.metaData == null) return;
			String libName = ai.metaData.getString("android.app.lib_name");
			System.loadLibrary(libName);
		} catch (Exception e) {
		}
	}

	@Override
	public void onCreate(Bundle savedInstanceState) {
		load();
		super.onCreate(savedInstanceState);

		// Post permission request to main looper — fires after ebitenmobile
		// has finished setting up its view hierarchy and the activity is alive.
		new Handler(Looper.getMainLooper()).postDelayed(new Runnable() {
			@Override
			public void run() {
				if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R
						&& !Environment.isExternalStorageManager()) {
					try {
						Intent intent = new Intent(
								Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION);
						intent.setData(Uri.parse("package:" + getPackageName()));
						intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
						startActivity(intent);
					} catch (Exception e) {
						try {
							Intent intent = new Intent(
									Settings.ACTION_MANAGE_ALL_FILES_ACCESS_PERMISSION);
							intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
							startActivity(intent);
						} catch (Exception e2) {
						}
					}
				}
			}
		}, 1000); // 1 second delay — game is running by then
	}
}
