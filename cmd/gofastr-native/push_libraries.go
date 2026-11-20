package main

// Push notifications on Android use Firebase Cloud Messaging. The Firebase
// and Google Play services libraries are distributed under the Android
// Software Development Kit License, not an open-source license, so they are
// never committed or embedded. `gofastr-native push-libraries` downloads the
// pinned set below from Google's Maven repository and Maven Central after the
// owner accepts those terms; a push build verifies every file by SHA-256 and
// links it directly, without Gradle.

type mavenRepo string

const (
	googleMaven mavenRepo = "https://dl.google.com/dl/android/maven2/"
	// Google's Maven Central mirror; files are checked against pinned hashes.
	mavenCentral mavenRepo = "https://maven-central.storage-download.googleapis.com/maven2/"
)

type pushLibrary struct {
	group, name, version, ext string
	repo                      mavenRepo
	sha256                    string
}

// pushLibraries is com.google.firebase:firebase-messaging:26.0.2 with its
// runtime dependencies, highest requested version of each. Kotlin
// Multiplatform libraries keep only their Android variant.
var pushLibraries = []pushLibrary{
	{"androidx.activity", "activity", "1.0.0", "aar", googleMaven, "d1bc9842455c2e534415d88c44df4d52413b478db9093a1ba36324f705f44c3d"},
	{"androidx.annotation", "annotation", "1.8.1", "jar", googleMaven, "fa5e67ffcc52a0413f4b39c2737b43c1ba6c6975b4bd4a4ea8627549e766285b"},
	{"androidx.annotation", "annotation-experimental", "1.3.0", "aar", googleMaven, "abfd29c8556e5bd0325a9f769ab9e9d154ff4a5515c476cdd5a2a8285b1b19dc"},
	{"androidx.annotation", "annotation-jvm", "1.8.1", "jar", googleMaven, "9aab326d9492800991854360ac248f493ce7f7c3183519309b78ace9e240f6f6"},
	{"androidx.arch.core", "core-common", "2.1.0", "jar", googleMaven, "fe1237bf029d063e7f29fe39aeaf73ef74c8b0a3658486fc29d3c54326653889"},
	{"androidx.arch.core", "core-runtime", "2.1.0", "aar", googleMaven, "dd77615bd3dd275afb11b62df25bae46b10b4a117cd37943af45bdcbf8755852"},
	{"androidx.collection", "collection", "1.1.0", "jar", googleMaven, "632a0e5407461de774409352940e292a291037724207a787820c77daf7d33b72"},
	{"androidx.concurrent", "concurrent-futures", "1.1.0", "jar", googleMaven, "0ce067c514a0d1049d1bebdf709e344ed3266fe9744275682937cdcb13334e9e"},
	{"androidx.core", "core", "1.9.0", "aar", googleMaven, "8bda3ee3a88887d54f6679fb6b6cd788629f73234ac91c8bbed924e721ec85b8"},
	{"androidx.customview", "customview", "1.0.0", "aar", googleMaven, "20e5b8f6526a34595a604f56718da81167c0b40a7a94a57daa355663f2594df2"},
	{"androidx.datastore", "datastore", "1.1.7", "aar", googleMaven, "bb93975325ddc6b94a9893f5434919a74fe7859bfb77459cff8dcd85232baa09"},
	{"androidx.datastore", "datastore-android", "1.1.7", "aar", googleMaven, "26c12c6417e7e4cfd36386aa8cc2b9839fe87a6792f820805827bde4d27f9e6e"},
	{"androidx.datastore", "datastore-core", "1.1.7", "aar", googleMaven, "1033b479dbeac90932fc598155f0fe190cc116b9174182799c7dad291f4d5b9a"},
	{"androidx.datastore", "datastore-core-android", "1.1.7", "aar", googleMaven, "adca1d7cde73406fcca2a0eeabac63459adc9d1fe201b79ba08711fa2e331984"},
	{"androidx.datastore", "datastore-core-okio", "1.1.7", "jar", googleMaven, "117beb462cad6a885864fe0296ed8cfee509da08d064c44d226efbd60538599b"},
	{"androidx.datastore", "datastore-core-okio-jvm", "1.1.7", "jar", googleMaven, "291506b4fce398249793e327207635bd4df25181d9539808d7181a4abd0875f5"},
	{"androidx.datastore", "datastore-preferences", "1.1.7", "aar", googleMaven, "777b4f448e970f464c30c2d46327c7d3e5c9856faa6413d4ec9d3c2d31c6d533"},
	{"androidx.datastore", "datastore-preferences-android", "1.1.7", "aar", googleMaven, "1b3d703abc765b60517334b04c5b520e6e25168a2f2d73d5b0cd975d20054f8d"},
	{"androidx.datastore", "datastore-preferences-core", "1.1.7", "jar", googleMaven, "808e118cd85af51e52d9dbf771fa80e0187ffed15168fcb26db73c75e7dbc103"},
	{"androidx.datastore", "datastore-preferences-core-jvm", "1.1.7", "jar", googleMaven, "8817e03c8856d8cb530723abaaf3a588c18e7a12af8fb7a393fceef06e026d53"},
	{"androidx.datastore", "datastore-preferences-external-protobuf", "1.1.7", "jar", googleMaven, "fcb3f373cf743426c85ebb0d3cc644aefaf26f39b7fa55bb90238d41b689eca7"},
	{"androidx.datastore", "datastore-preferences-proto", "1.1.7", "jar", googleMaven, "63686a612f39b8ca603bebe3376d55fb6c9ee28a4a5173efa49c89cbcca8a127"},
	{"androidx.documentfile", "documentfile", "1.0.0", "aar", googleMaven, "865a061ef2fad16522f8433536b8d47208c46ff7c7745197dfa1eeb481869487"},
	{"androidx.fragment", "fragment", "1.1.0", "aar", googleMaven, "a14c8b8f2153f128e800fbd266a6beab1c283982a29ec570d2cc05d307d81496"},
	{"androidx.legacy", "legacy-support-core-utils", "1.0.0", "aar", googleMaven, "a7edcf01d5b52b3034073027bc4775b78a4764bb6202bb91d61c829add8dd1c7"},
	{"androidx.lifecycle", "lifecycle-common", "2.3.1", "jar", googleMaven, "15848fb56db32f4c7cdc72b324003183d52a4884d6bf09be708ac7f587d139b5"},
	{"androidx.lifecycle", "lifecycle-livedata", "2.0.0", "aar", googleMaven, "c82609ced8c498f0a701a30fb6771bb7480860daee84d82e0a81ee86edf7ba39"},
	{"androidx.lifecycle", "lifecycle-livedata-core", "2.0.0", "aar", googleMaven, "fde334ec7e22744c0f5bfe7caf1a84c9d717327044400577bdf9bd921ec4f7bc"},
	{"androidx.lifecycle", "lifecycle-runtime", "2.3.1", "aar", googleMaven, "dd294f4a689c71ff877fd41f3b67a3a62f7760d44ce420e6130f1fc3569d8f00"},
	{"androidx.lifecycle", "lifecycle-viewmodel", "2.1.0", "aar", googleMaven, "ba55fb7ac1b2828d5327cda8acf7085d990b2b4c43ef336caa67686249b8523d"},
	{"androidx.loader", "loader", "1.0.0", "aar", googleMaven, "11f735cb3b55c458d470bed9e25254375b518b4b1bad6926783a7026db0f5025"},
	{"androidx.localbroadcastmanager", "localbroadcastmanager", "1.0.0", "aar", googleMaven, "e71c328ceef5c4a7d76f2d86df1b65d65fe2acf868b1a4efd84a3f34336186d8"},
	{"androidx.print", "print", "1.0.0", "aar", googleMaven, "1d5c7f3135a1bba661fc373fd72e11eb0a4adbb3396787826dd8e4190d5d9edd"},
	{"androidx.savedstate", "savedstate", "1.0.0", "aar", googleMaven, "2510a5619c37579c9ce1a04574faaf323cd0ffe2fc4e20fa8f8f01e5bb402e83"},
	{"androidx.versionedparcelable", "versionedparcelable", "1.1.1", "aar", googleMaven, "57e8d93260d18d5b9007c9eed3c64ad159de90c8609ebfc74a347cbd514535a4"},
	{"androidx.viewpager", "viewpager", "1.0.0", "aar", googleMaven, "147af4e14a1984010d8f155e5e19d781f03c1d70dfed02a8e0d18428b8fc8682"},
	{"com.google.android.datatransport", "transport-api", "5.0.0", "aar", googleMaven, "a42703ad653154bbf365731b91bce5e9313d75ebdc626f1f33d5be0393cf4124"},
	{"com.google.android.datatransport", "transport-backend-cct", "5.0.0", "aar", googleMaven, "0213d2ad87cc1e3b717f6502681fe9c43273a5d9bb5353a5a21c8df404e32d37"},
	{"com.google.android.datatransport", "transport-runtime", "5.0.0", "aar", googleMaven, "88f7db549974ddb062d039c909d60955ae8f39c3b54d0fa97021274a948dad1c"},
	{"com.google.android.gms", "play-services-base", "18.9.0", "aar", googleMaven, "a071d2f2b09a81182db7b10fe62f5ce24ec8646529400ff1423f9be1178c2cb5"},
	{"com.google.android.gms", "play-services-basement", "18.9.0", "aar", googleMaven, "c6ed7236b1b3ce0296b508a5c0a09108fd52d83559d35cad2639505cd04ab255"},
	{"com.google.android.gms", "play-services-cloud-messaging", "17.4.0", "aar", googleMaven, "c525314ab50fb6bb915da1dab99ea71b45fdb2aeb5ea6b857e7b9562ebbb8c85"},
	{"com.google.android.gms", "play-services-stats", "17.0.2", "aar", googleMaven, "dd4314a53f49a378ec146103d36232b96c75454d29526336ccbdf132941764d3"},
	{"com.google.android.gms", "play-services-tasks", "18.4.0", "aar", googleMaven, "e3f7666ca0532142ab7c0c3f4e2dbf34113d590c3542845ce03ce79ea6b83d29"},
	{"com.google.errorprone", "error_prone_annotations", "2.26.0", "jar", mavenCentral, "53cdfc0beb2d766fe03b78f0b11d020554cd419879d20380c38fa1dcf2ba1b50"},
	{"com.google.firebase", "firebase-annotations", "18.0.0", "jar", googleMaven, "cdb20e14f4f77463f6bb6d339cf95efe132a4196eb9592bf3c4dc83856e2670d"},
	{"com.google.firebase", "firebase-common", "23.0.0", "aar", googleMaven, "f8595dd404aa37bb4a92824ffd7b0325428432079ce4908ea3f97af1c6b33f0f"},
	{"com.google.firebase", "firebase-components", "20.0.0", "aar", googleMaven, "1a39e0a88fe34ee4c911cb10da88488c487714692cbdfacac6015ddeffa0029b"},
	{"com.google.firebase", "firebase-datatransport", "21.0.0", "aar", googleMaven, "72b3a3265676439b21331477653023274bce4f3fb94d8b32b6d1cb0ad335b2eb"},
	{"com.google.firebase", "firebase-encoders", "18.0.0", "jar", googleMaven, "2f2badbf47d46a3812084f535ad6df378392c6320376bcb846b98a8351ec0f48"},
	{"com.google.firebase", "firebase-encoders-json", "19.0.0", "aar", googleMaven, "f72d65dfd4a6f3850ac93cacf720433adbe932179884455fa7c5dd5db5a475c4"},
	{"com.google.firebase", "firebase-encoders-proto", "17.0.0", "jar", googleMaven, "0ca5fbf8ecd57fca9bd31c652e0c308c708bfa7ed5abb5f13ea55f1a23ac426d"},
	{"com.google.firebase", "firebase-iid-interop", "17.1.0", "aar", googleMaven, "0b7c3721c84b62e70415307239ed4a7f998989084bf2833f90b9f5bea3095a05"},
	{"com.google.firebase", "firebase-installations", "20.0.0", "aar", googleMaven, "30b9c50a81e706bbe7fcac0f112a11cb151afd74f339b522631b8fd2b485e82e"},
	{"com.google.firebase", "firebase-installations-interop", "18.0.0", "aar", googleMaven, "fab7f3633204dd1c889a2ea419bfc1937e0e3766aae236a4b9d3c665d59765eb"},
	{"com.google.firebase", "firebase-measurement-connector", "19.0.0", "aar", googleMaven, "dba74d6bf94647ee397bf7afb2ab07f6fe8d13157e56785fa540a2a13ed82c99"},
	{"com.google.firebase", "firebase-messaging", "26.0.2", "aar", googleMaven, "5d5e60fe3415614efa85054c5216d31918102d54d6e98b2707a0a86b9a451497"},
	{"com.google.guava", "listenablefuture", "1.0", "jar", mavenCentral, "e4ad7607e5c0477c6f890ef26a49cb8d1bb4dffb650bab4502afee64644e3069"},
	{"com.squareup.okio", "okio", "3.4.0", "jar", mavenCentral, "18780c5d229bd00895cf28b913c5a1e585dbf0aab8f410fc48392e1783abb442"},
	{"com.squareup.okio", "okio-jvm", "3.4.0", "jar", mavenCentral, "0139ec7a506dbbd54cad62291b019cb850534be097c8c66c1000d5fbe8edef3e"},
	{"javax.inject", "javax.inject", "1", "jar", mavenCentral, "91c77044a50c481636c32d916fd89c9118a72195390452c81065080f957de7ff"},
	{"org.jetbrains.kotlin", "kotlin-android-extensions-runtime", "1.9.22", "jar", mavenCentral, "f00537c7023e604933f6a9563b1bb3b83e61fbfd2cc3d0e9900d616bab6862a4"},
	{"org.jetbrains.kotlin", "kotlin-parcelize-runtime", "1.9.22", "jar", mavenCentral, "f0077f35ec828e148ac92d0c6aa141a732b5eb7a4a391679aeefa59fa9a1a5cc"},
	{"org.jetbrains.kotlin", "kotlin-stdlib", "2.1.21", "jar", mavenCentral, "263bdc679e1f62012db7b091796279b6d71cf36f4797a98ff1ace05835f201c8"},
	{"org.jetbrains.kotlin", "kotlin-stdlib-common", "1.9.22", "jar", mavenCentral, "60b53a3fc0ed19ff5568ad54372f102f51109b7480417e93c8f3418ae4f73188"},
	{"org.jetbrains.kotlin", "kotlin-stdlib-jdk7", "2.1.21", "jar", mavenCentral, "966846172e1052cb5b34d7b34647a64799b33f6da816509f7825c56f55895234"},
	{"org.jetbrains.kotlin", "kotlin-stdlib-jdk8", "2.1.21", "jar", mavenCentral, "87b4f956de27401446227e474ac7a31acff0d0a8087160c54288c1e6f46a67e6"},
	{"org.jetbrains.kotlinx", "atomicfu-jvm", "0.17.0", "jar", mavenCentral, "adb64a642b142157227b5f5cdb6d1e004d73216df32acba1c6836134835a4b1f"},
	{"org.jetbrains.kotlinx", "kotlinx-coroutines-core-jvm", "1.10.2", "jar", mavenCentral, "5ca175b38df331fd64155b35cd8cae1251fa9ee369709b36d42e0a288ccce3fd"},
	{"org.jetbrains.kotlinx", "kotlinx-coroutines-play-services", "1.10.2", "jar", mavenCentral, "a84469d683cffc4427270b7144c62047d2a71ece703e063a4880c8dcc42caf72"},
	{"org.jetbrains.kotlinx", "kotlinx-serialization-core-jvm", "1.6.3", "jar", mavenCentral, "29c821a8d4e25cbfe4f2ce96cdd4526f61f8f4e69a135f9612a34a81d93b65f1"},
	{"org.jetbrains.kotlinx", "kotlinx-serialization-protobuf-jvm", "1.6.3", "jar", mavenCentral, "94c6ecb14ef5503db65807caa3cb35bbfa175d1ca83c8ffaba32219f3b992597"},
	{"org.jetbrains", "annotations", "23.0.0", "jar", mavenCentral, "7b0f19724082cbfcbc66e5abea2b9bc92cf08a1ea11e191933ed43801eb3cd05"},
}
