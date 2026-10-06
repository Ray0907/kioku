#!/usr/bin/env python3
"""Small, dependency-free writer for this two-target Xcode project."""
import pathlib

root = pathlib.Path(__file__).resolve().parent
objects = {}
def add(kind, **values):
    key = f'{len(objects)+1:024X}'
    objects[key] = dict(isa=kind, **values)
    return key
def source(path, kind):
    ref = add('PBXFileReference', lastKnownFileType=kind, path=path, sourceTree='<group>')
    return ref, add('PBXBuildFile', fileRef=ref)
def config(name, settings):
    return add('XCBuildConfiguration', name=name, buildSettings=settings)
def configs(settings):
    return add('XCConfigurationList', buildConfigurations=[config(n, dict(settings, SWIFT_OPTIMIZATION_LEVEL=('-Onone' if n=='Debug' else '-O'), SWIFT_ACTIVE_COMPILATION_CONDITIONS=('$(inherited) DEBUG' if n=='Debug' else '$(inherited)'))) for n in ['Debug','Release']], defaultConfigurationIsVisible=0, defaultConfigurationName='Release')

app_sources = [source(p,'sourcecode.swift') for p in ['Kioku/App.swift','Kioku/Engine.swift','Kioku/Actions.swift','Kioku/DisplayMode.swift','Kioku/TextLayout.swift']]
test_sources = [source('test/UITests.swift','sourcecode.swift')]
localizations = [add('PBXFileReference', lastKnownFileType='text.plist.strings', name=lang, path=f'Kioku/{lang}.lproj/Localizable.strings', sourceTree='<group>') for lang in ['en','zh-Hant']]
strings = add('PBXVariantGroup', name='Localizable.strings', children=localizations, sourceTree='<group>')
strings_build = add('PBXBuildFile', fileRef=strings)
resources = add('PBXResourcesBuildPhase', buildActionMask=2147483647, files=[strings_build], runOnlyForDeploymentPostprocessing=0)
app_product = add('PBXFileReference', explicitFileType='wrapper.application', path='Kioku.app', sourceTree='BUILT_PRODUCTS_DIR')
test_product = add('PBXFileReference', explicitFileType='wrapper.cfbundle', path='KiokuUITests.xctest', sourceTree='BUILT_PRODUCTS_DIR')
products = add('PBXGroup', name='Products', children=[app_product,test_product], sourceTree='<group>')
group = add('PBXGroup', children=[x[0] for x in app_sources+test_sources]+[strings,products], sourceTree='<group>')
app_build = add('PBXSourcesBuildPhase', buildActionMask=2147483647, files=[x[1] for x in app_sources], runOnlyForDeploymentPostprocessing=0)
engine_build = add('PBXShellScriptBuildPhase', name='Bundle Go engine', buildActionMask=2147483647, files=[], inputPaths=[], outputPaths=[], runOnlyForDeploymentPostprocessing=0, shellPath='/bin/bash', shellScript='bash "$SRCROOT/build-engine.sh"', alwaysOutOfDate=1)
settings = dict(PRODUCT_NAME='$(TARGET_NAME)', PRODUCT_BUNDLE_IDENTIFIER='com.kioku.mac', INFOPLIST_FILE='Kioku/Info.plist', SWIFT_VERSION='6.0', SWIFT_STRICT_CONCURRENCY='complete', CODE_SIGN_IDENTITY='-', CODE_SIGN_STYLE='Manual', ENABLE_APP_SANDBOX='NO', ENABLE_USER_SCRIPT_SANDBOXING='NO', COMBINE_HIDPI_IMAGES='YES')
app_target = add('PBXNativeTarget', buildConfigurationList=configs(settings), buildPhases=[app_build,resources,engine_build], buildRules=[], dependencies=[], name='Kioku', productName='Kioku', productReference=app_product, productType='com.apple.product-type.application')
test_build = add('PBXSourcesBuildPhase', buildActionMask=2147483647, files=[x[1] for x in test_sources], runOnlyForDeploymentPostprocessing=0)
test_settings = dict(PRODUCT_NAME='$(TARGET_NAME)', PRODUCT_BUNDLE_IDENTIFIER='com.kioku.mac.uitests', GENERATE_INFOPLIST_FILE='YES', SWIFT_VERSION='6.0', CODE_SIGN_IDENTITY='-', CODE_SIGN_STYLE='Manual', TEST_TARGET_NAME='Kioku', INFOPLIST_KEY_KiokuFixtureHome='$(KIOKU_E2E_HOME)')
test_target = add('PBXNativeTarget', buildConfigurationList=configs(test_settings), buildPhases=[test_build], buildRules=[], dependencies=[], name='KiokuUITests', productName='KiokuUITests', productReference=test_product, productType='com.apple.product-type.bundle.ui-testing')
project = add('PBXProject', attributes=dict(LastUpgradeCheck='2600', TargetAttributes={test_target:dict(TestTargetID=app_target)}), buildConfigurationList=configs(dict(SDKROOT='macosx', MACOSX_DEPLOYMENT_TARGET='14.0', CLANG_ENABLE_MODULES='YES')), compatibilityVersion='Xcode 14.0', developmentRegion='en', knownRegions=['en','zh-Hant','Base'], mainGroup=group, productRefGroup=products, projectDirPath='', projectRoot='', targets=[app_target,test_target])
def encode(value):
    if isinstance(value,dict): return '{ '+ ' '.join(f'{encode(k)} = {encode(v)};' for k,v in value.items())+' }'
    if isinstance(value,list): return '('+', '.join(encode(x) for x in value)+')'
    if isinstance(value,int): return str(value)
    return '"'+str(value).replace('\\','\\\\').replace('"','\\"').replace('\n','\\n')+'"'
project_dir = root/'Kioku.xcodeproj'
project_dir.mkdir(exist_ok=True)
(project_dir/'project.pbxproj').write_text('// !$*UTF8*$!\n'+encode(dict(archiveVersion=1,classes={},objectVersion=56,objects=objects,rootObject=project))+'\n')
def reference(target,name):
    return f'<BuildableReference BuildableIdentifier="primary" BlueprintIdentifier="{target}" BuildableName="{name}" BlueprintName="{name.split(".")[0]}" ReferencedContainer="container:Kioku.xcodeproj"/>'
schemes = project_dir/'xcshareddata/xcschemes'
schemes.mkdir(parents=True,exist_ok=True)
(schemes/'Kioku.xcscheme').write_text(f'''<?xml version="1.0" encoding="UTF-8"?>
<Scheme LastUpgradeVersion="2600" version="1.3">
<BuildAction parallelizeBuildables="YES" buildImplicitDependencies="YES"><BuildActionEntries>
<BuildActionEntry buildForTesting="YES" buildForRunning="YES" buildForProfiling="YES" buildForArchiving="YES" buildForAnalyzing="YES">{reference(app_target,'Kioku.app')}</BuildActionEntry>
<BuildActionEntry buildForTesting="YES" buildForRunning="NO" buildForProfiling="NO" buildForArchiving="NO" buildForAnalyzing="NO">{reference(test_target,'KiokuUITests.xctest')}</BuildActionEntry>
</BuildActionEntries></BuildAction>
<TestAction buildConfiguration="Debug" selectedDebuggerIdentifier="Xcode.DebuggerFoundation.Debugger.LLDB" selectedLauncherIdentifier="Xcode.IDEFoundation.Launcher.LLDB" shouldUseLaunchSchemeArgsEnv="YES"><Testables><TestableReference skipped="NO">{reference(test_target,'KiokuUITests.xctest')}</TestableReference></Testables></TestAction>
<LaunchAction buildConfiguration="Debug" selectedDebuggerIdentifier="Xcode.DebuggerFoundation.Debugger.LLDB" selectedLauncherIdentifier="Xcode.IDEFoundation.Launcher.LLDB" launchStyle="0" useCustomWorkingDirectory="NO" ignoresPersistentStateOnLaunch="NO" debugDocumentVersioning="YES"><BuildableProductRunnable runnableDebuggingMode="0">{reference(app_target,'Kioku.app')}</BuildableProductRunnable></LaunchAction>
<AnalyzeAction buildConfiguration="Debug"/><ArchiveAction buildConfiguration="Release" revealArchiveInOrganizer="YES"/>
</Scheme>''')
