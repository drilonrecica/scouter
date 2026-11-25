plugins {
    id("com.android.application")
}

android {
    namespace = "dev.recica.scouter"
    compileSdk = 35

    defaultConfig {
        applicationId = "dev.recica.scouter"
        minSdk = 28
        // The only device runs Android 12 (API 32); targeting it keeps
        // platform behaviour exactly what we test against.
        targetSdk = 32
        versionCode = 14
        versionName = "0.4.1"
        ndk { abiFilters += "armeabi-v7a" }
    }

    // Sideloaded onto one phone, never on Play: the debug key is fine. Set
    // SCOUTER_KEYSTORE_FILE (+ _PASSWORD, SCOUTER_KEY_ALIAS, SCOUTER_KEY_PASSWORD)
    // to sign with another key instead, as the release workflow does when the
    // keystore secrets exist. Android updates in place only over the same key.
    val keystoreFile = System.getenv("SCOUTER_KEYSTORE_FILE")
    if (keystoreFile != null) {
        signingConfigs.create("env") {
            storeFile = file(keystoreFile)
            storePassword = System.getenv("SCOUTER_KEYSTORE_PASSWORD")
            keyAlias = System.getenv("SCOUTER_KEY_ALIAS")
            keyPassword = System.getenv("SCOUTER_KEY_PASSWORD")
        }
    }
    buildTypes {
        release {
            isMinifyEnabled = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"))
            signingConfig = signingConfigs.getByName(if (keystoreFile != null) "env" else "debug")
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    lint {
        abortOnError = true
        disable += "ExpiredTargetSdkVersion" // sideloaded, never on Play
    }
}

dependencies {
    testImplementation("junit:junit:4.13.2")
    // android.jar's org.json is a stub in JVM unit tests; use the real one there.
    testImplementation("org.json:json:20250517")
}
