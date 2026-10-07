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
        versionCode = 2
        versionName = "0.2.0"
        ndk { abiFilters += "armeabi-v7a" }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"))
            // Sideloaded onto one phone: the debug key is fine.
            signingConfig = signingConfigs.getByName("debug")
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    lint {
        abortOnError = false
        disable += "ExpiredTargetSdkVersion" // sideloaded, never on Play
    }
}

dependencies {
    testImplementation("junit:junit:4.13.2")
    // android.jar's org.json is a stub in JVM unit tests; use the real one there.
    testImplementation("org.json:json:20250517")
}
